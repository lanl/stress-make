// Run a process-creation server and point our customized GNU Make at it.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strings"
	"sync/atomic"
	"syscall"
	"time"
)

// Define a dequeue order.
type DequeueOrder int

// Define the order that we run enqueued processes.
const (
	FIFOOrder   = DequeueOrder(iota) // First in, first out
	LIFOOrder                        // Last in, first out
	RandomOrder                      // Random order
)

// deqOrder specifies the order in which we dequeue commands to run.
var deqOrder DequeueOrder

// maxLiveChildren specifies the maximum number of children we allow
// to execute concurrently.
var maxLiveChildren int64 = 1

// currentLiveChildren specifies the number of children that are
// currently executing.  The initial GNU Make process counts as 1.
var currentLiveChildren int64 = 1

// When verbose is true, stress-make outputs additional information about its
// activities.
var verbose bool

// stats keeps track of various interesting statistics regarding the build.
var stats *Statistics = NewStatistics()

// As in C, a pid_t represents a process ID (real or fake).
type pid_t int

// maxFakePid is the maximum value allowed for a fake PID.
const maxFakePid pid_t = 2147483647

// ChildCmd extends *exec.Cmd with a fake PID to return to GNU Make.
type ChildCmd struct {
	*exec.Cmd
	FakePid pid_t
}

// completedCommands reports children that have completed on a per-PID
// basis (i.e., PID of the GNU Make process requesting the child).
var completedCommands map[pid_t]chan ChildCmd

// fakePidToCmd maps a fabricated process ID to the associated command
// structure.
var fakePidToCmd map[pid_t]*exec.Cmd

// nextFakePid is the next available fake process ID we should return.
var nextFakePid pid_t = 1

// prng is a pseudorandom-number generator.
var prng *rand.Rand

// An empty represents zero-byte storage.
type empty struct{}

// createSocket chooses a random name for a Unix-domain socket and opens it.
func createSocket() (string, *net.UnixListener) {
	// Define the parameters for name creation.
	const numChars = 6 // Number of random characters to include
	const charSet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	const charSetLen = len(charSet)
	baseName := path.Join(os.TempDir(), "stress-make-")

	// Repeatedly choose names until socket creation is successful.
	for {
		// Randomly construct a socket name.
		sockName := baseName
		for i := 0; i < numChars; i++ {
			sockName += string(charSet[prng.Intn(charSetLen)])
		}

		// Try creating a socket with that name.  Return if
		// successful; try again if not.
		addr, err := net.ResolveUnixAddr("unix", sockName)
		if err != nil {
			continue
		}
		listener, err := net.ListenUnix("unix", addr)
		if err == nil {
			return sockName, listener
		}
	}
}

// mergeArgs merges an argument list into a single string.
func mergeArgs(args []string) string {
	quotedArgs := make([]string, len(args))
	for i, arg := range args {
		if len(arg) > 0 && !strings.ContainsAny(arg, "'` $\"\n\t\r") {
			quotedArgs[i] = arg
		} else {
			quotedArgs[i] = "'" + strings.Replace(arg, "'", `'\''`, -1) + "'"
		}
	}
	return strings.Join(quotedArgs, " ")
}

// A RequestType is used internally to identify what a GNU Make
// process is requesting.
type RequestType int

const (
	HelloRequest   = RequestType(iota + 1) // New GNU Make process says hello
	EnqueueRequest                         // Enqueue a command to run sometime
	WaitRequest                            // Poll or block for an enqueued command to complete
	KillRequest                            // Terminate an enqueued or running command
)

// A queueRequest is used internally to request updates to global data
// structures.  MakePid and RespChan must always be specified.  If neither
// Command nor Killpid are specified, this requests that a pending shell
// command be run.
type queueRequest struct {
	Request  RequestType // Action to perform
	MakePid  pid_t       // Process ID (from a GNU Make process) that initiated the request
	Command  *exec.Cmd   // Command to enqueue on the pending queue
	KillPid  pid_t       // (Fake) process ID to kill
	RespChan chan pid_t  // Response (success code or PID)
}

// qReqChan is the channel used to talk to the single instance of UpdateState.
var qReqChan chan queueRequest

// updateStateHello is called by UpdateState upon receiving a Hello request.
func updateStateHello(qReq *queueRequest) {
	// Allocate a fake PID for a new GNU Make process.
	mkPid := qReq.MakePid
	fPid := nextFakePid
	if nextFakePid == maxFakePid {
		log.Fatalf("Too many processes (> %d)", maxFakePid)
	}
	nextFakePid++
	stats.ObserveSpawn(mkPid, fPid)
	qReq.RespChan <- fPid
	close(qReq.RespChan)
}

// updateStateEnqueue is called by UpdateState upon receiving an Enqueue
// request.
func updateStateEnqueue(qReq *queueRequest, allPendingCommands *[]pid_t, pidPendingCommands map[pid_t]map[pid_t]empty) {
	// Allocate a new fake PID.
	fPid := nextFakePid
	fakePidToCmd[fPid] = qReq.Command
	if nextFakePid == maxFakePid {
		log.Fatalf("Too many processes (> %d)", maxFakePid)
	}
	nextFakePid++

	// Set the fake PID in the command's environment.
	elide := -1
	newEnv := qReq.Command.Env
	for i, eVar := range newEnv {
		if strings.HasPrefix(eVar, "STRESSMAKE_FAKE_PID=") {
			elide = i
			break
		}
	}
	if elide == -1 {
		log.Fatal("Internal error: fake PID not set")
	}
	oldLen := len(newEnv)
	newEnv[elide] = newEnv[oldLen-1]
	newEnv = newEnv[:oldLen-1]
	qReq.Command.Env = append(newEnv, fmt.Sprintf("STRESSMAKE_FAKE_PID=%d", fPid))

	// Enqueue a new pending command on both the global list and the
	// per-PID set.
	*allPendingCommands = append(*allPendingCommands, fPid)
	mkPid := qReq.MakePid
	pidMap, ok := pidPendingCommands[mkPid]
	if !ok {
		// This is the first request from PID MakePid.
		pidMap = make(map[pid_t]empty, maxLiveChildren)
		pidPendingCommands[mkPid] = pidMap
	}
	pidMap[fPid] = *&empty{}

	// For reporting purposes, keep track of the maximium number of pending
	// + running commands that existed at any one time and of the total
	// number of commands enqueued.
	nConc := int64(len(*allPendingCommands)) + atomic.LoadInt64(&currentLiveChildren) - 1 // -1 because of GNU Make itself
	stats.ObserveConcurrency(nConc)

	// Return the fake PID.
	qReq.RespChan <- fPid
	close(qReq.RespChan)
}

// updateStateKill is called by UpdateState upon receiving a Kill request.
func updateStateKill(qReq *queueRequest, allPendingCommands *[]pid_t, pidPendingCommands map[pid_t]map[pid_t]empty) {
	// Kill the job if it's running.
	fPid := qReq.KillPid
	cmd, ok := fakePidToCmd[fPid]
	if !ok {
		// Error case (invalid fake PID)
		qReq.RespChan <- 0
		close(qReq.RespChan)
		return
	}
	if cmd.Process != nil {
		// Job has launched.  Kill it.
		if cmd.Process.Signal(os.Kill) != nil {
			ok = false
		}
	}

	// Whether or not the job has launched, elide the command from
	// allPendingCommands, and delete it from pidPendingCommands.  Also
	// delete the association between the fake PID and the command.
	delete(fakePidToCmd, fPid)
	for i, pendingPid := range *allPendingCommands {
		if pendingPid == fPid {
			*allPendingCommands = append((*allPendingCommands)[:i], (*allPendingCommands)[i+1:]...)
			delete(pidPendingCommands[qReq.MakePid], fPid)
			break
		}
	}
	if ok {
		qReq.RespChan <- 1
	} else {
		qReq.RespChan <- 0
	}
	close(qReq.RespChan)
}

// updateStateWait is called by UpdateState upon receiving a Wait request.
func updateStateWait(qReq *queueRequest, allPendingCommands *[]pid_t, pidPendingCommands map[pid_t]map[pid_t]empty) {
	// Ensure we have a command to run.
	nPending := len(*allPendingCommands)
	if nPending == 0 {
		if atomic.LoadInt64(&currentLiveChildren) == 0 {
			// Something is seriously wrong.  Make thought it had
			// pending commands, but it didn't.
			qReq.RespChan <- 0
		} else {
			// We have nothing new to run so the caller just needs
			// to wait for the currently running commands to
			// finish.
			qReq.RespChan <- 1
		}
		close(qReq.RespChan)
		return
	}

	// Run as many commands as we can.
	for nPending > 0 && atomic.LoadInt64(&currentLiveChildren) < maxLiveChildren {
		// Select a pending command to run.
		var idx int
		switch deqOrder {
		case FIFOOrder:
			idx = 0
		case LIFOOrder:
			idx = nPending - 1
		case RandomOrder:
			idx = prng.Intn(nPending)
		default:
			log.Fatal("Internal error processing the dequeue order")
		}

		// Elide the command from allPendingCommands, and delete it
		// from pidPendingCommands.
		mkPid := qReq.MakePid
		fPid := (*allPendingCommands)[idx]
		cmd := fakePidToCmd[fPid]
		*allPendingCommands = append((*allPendingCommands)[:idx], (*allPendingCommands)[idx+1:]...)
		delete(pidPendingCommands[mkPid], fPid)
		nPending--

		// Create a completion queue if necessary.
		if _, ok := completedCommands[mkPid]; !ok {
			completedCommands[mkPid] = make(chan ChildCmd, maxLiveChildren)
		}

		// Run the command in the background.
		atomic.AddInt64(&currentLiveChildren, 1)
		go func() {
			// Run the command then "kill" it to remove it from
			// fakePidToCmd.
			if verbose {
				log.Printf("INFO: Running enqueued job (ID %d): %s", fPid, mergeArgs(cmd.Args))
			}
			beginTime := time.Now()
			err := cmd.Run()
			if err != nil && verbose {
				log.Printf("INFO: Job ID %d failed (%s)", fPid, err)
			}
			stats.ObserveExecution(fPid, mkPid, time.Since(beginTime))
			atomic.AddInt64(&currentLiveChildren, -1)

			qRespChan := make(chan pid_t)
			qReqChan <- queueRequest{Request: KillRequest, MakePid: mkPid, KillPid: fPid, RespChan: qRespChan}
			_ = <-qRespChan
			completedCommands[mkPid] <- ChildCmd{Cmd: cmd, FakePid: fPid}
		}()
	}
	qReq.RespChan <- 1
	close(qReq.RespChan)
}

// UpdateState serially updates global state in a concurrent context.  Much of
// what the program actually does is encapsulated in the UpdateState routine.
func UpdateState() {
	allPendingCommands := make([]pid_t, 0, maxLiveChildren) // All pending commands (by fake PID) across all Make processes
	pidPendingCommands := make(map[pid_t]map[pid_t]empty)   // Subset of allPendingCommands local to a Make PID
	for qReq := range qReqChan {
		if qReq.RespChan == nil {
			log.Fatal("Internal error processing a queue request")
		}
		switch qReq.Request {
		case HelloRequest:
			updateStateHello(&qReq)
		case EnqueueRequest:
			updateStateEnqueue(&qReq, &allPendingCommands, pidPendingCommands)
		case KillRequest:
			updateStateKill(&qReq, &allPendingCommands, pidPendingCommands)
		case WaitRequest:
			updateStateWait(&qReq, &allPendingCommands, pidPendingCommands)
		default:
			log.Fatal("Internal error -- bad request type")
		}

	}
}

// A RemoteQuery represents a query received from a socket.
type RemoteQuery struct {
	Request string   // Either "hello", "spawn", "status", or "kill"
	Args    []string // spawn: Command to run plus all arguments
	Environ []string // spawn: Environment to use (list of key=value pairs)
	Block   bool     // status: true=wait; false=return immediately
	Victim  pid_t    // kill: Victim process ID (really a fake PID)
	Pid     pid_t    // all: GNU Make process ID (really a fake PID)
}

// killProcess sends a kill signal to a pending or running process.
func helloFromMake(query *RemoteQuery, conn *net.UnixConn) {
	// Request a new fake PID and send that to GNU Make.
	qRespChan := make(chan pid_t)
	qReqChan <- queueRequest{Request: HelloRequest, MakePid: query.Pid, RespChan: qRespChan}
	fmt.Fprint(conn, <-qRespChan)
}

// enqueueCommand prepares a user-specified command for execution and
// enqueues it on the pendingCommands queue.  It returns a (fake) PID.
func enqueueCommand(query *RemoteQuery, conn *net.UnixConn) {
	if query.Args == nil || len(query.Args) == 0 {
		log.Fatal("Received a spawn request with empty arguments")
	}
	if query.Environ == nil || len(query.Environ) == 0 {
		log.Fatal("Received a spawn request with an empty environment")
	}
	if query.Pid == 0 {
		log.Fatal("Received a spawn request with an unspecified PID")
	}
	cmd := exec.Command(query.Args[0], query.Args[1:]...)
	cmd.Env = query.Environ
	for _, keyval := range query.Environ {
		if strings.HasPrefix(keyval, "PWD=") {
			cmd.Dir = keyval[4:]
			break
		}
	}
	if cmd.Dir == "" {
		log.Fatal("Spawn request did not include PWD in the environment")
	}
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	qRespChan := make(chan pid_t)
	qReqChan <- queueRequest{Request: EnqueueRequest, MakePid: query.Pid, Command: cmd, RespChan: qRespChan}
	fmt.Fprint(conn, <-qRespChan)
}

// awaitCommand polls or blocks for an arbitrary shell command to finish and
// sends some status information on a given socket.  If no command is running,
// choose one to run.
func awaitCommand(query *RemoteQuery, conn *net.UnixConn) {
	// Sanity-check the query.
	if query.Pid == 0 {
		log.Fatal("Received a status request with an unspecified PID")
	}
	mkPid := query.Pid

	// Launch new commands as lazily as possible.
	if query.Block {
		// If GNU Make blocks, it no longer counts as live.
		atomic.AddInt64(&currentLiveChildren, -1)
		defer atomic.AddInt64(&currentLiveChildren, 1)

		// Launch as many new commands as possible,
		qRespChan := make(chan pid_t)
		qReqChan <- queueRequest{Request: WaitRequest, MakePid: mkPid, RespChan: qRespChan}
		if <-qRespChan == 0 {
			// Something went wrong -- probably that there are no
			// new commands to run.  Notify the caller.
			fmt.Fprint(conn, "-1 0 0 0")
			return
		}
	}

	// If we were asked to block, then block until a running command
	// finishes.  Otherwise, simply test if any of our running commands
	// finished.
	var cmd ChildCmd
	if query.Block {
		cmd = <-completedCommands[mkPid]
	} else {
		select {
		case cmd = <-completedCommands[mkPid]:
		default:
		}
	}
	if cmd.FakePid != 0 {
		// A command finished -- send a reply with exit information.
		status, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
		if !ok {
			log.Fatal("Failed to retrieve the Unix wait status")
		}
		exitStatus := 0
		if status.Exited() {
			exitStatus = status.ExitStatus()
		}
		stopSignal := 0
		if status.Signaled() {
			stopSignal = int(status.StopSignal())
		}
		dumpedCore := 0
		if status.CoreDump() {
			dumpedCore = 1
		}
		fmt.Fprintf(conn, "%d %d %d %d",
			cmd.FakePid, exitStatus, stopSignal, dumpedCore)
		return
	}

	// No command finished.  Tell the caller to be patient.
	fmt.Fprint(conn, "0 0 0 0")
}

// killProcess sends a kill signal to a pending or running process.
func killProcess(query *RemoteQuery, conn *net.UnixConn) {
	// Sanity-check the query.
	if query.Pid == 0 {
		log.Fatal("Received a kill request with an unspecified PID")
	}
	mkPid := query.Pid
	if query.Victim == 0 {
		log.Fatal("Received a kill request with an unspecified victim PID")
	}
	fPid := query.Victim

	// Kill the process.
	qRespChan := make(chan pid_t)
	qReqChan <- queueRequest{Request: KillRequest, MakePid: mkPid, KillPid: fPid, RespChan: qRespChan}
	if <-qRespChan == 0 {
		// Something went wrong -- probably that there the victim PID
		// is invalid.
		fmt.Fprint(conn, "-1")
	} else {
		// The process can now be considered dead.
		fmt.Fprint(conn, "0")
	}
}

// processQuery reads a single query from a socket and, if
// appropriate, sends back a response.
func processQuery(conn *net.UnixConn) {
	// Read a query from the given socket.
	defer conn.Close()
	jsonDec := json.NewDecoder(conn)
	var query RemoteQuery
	jsonDec.Decode(&query)

	// Process the query.
	switch query.Request {
	case "hello":
		helloFromMake(&query, conn)
	case "spawn":
		enqueueCommand(&query, conn)
	case "status":
		awaitCommand(&query, conn)
	case "kill":
		killProcess(&query, conn)
	default:
		log.Fatalf("Received unrecognized request %q", query.Request)
	}
}

// serverLoop repeatedly accepts queries from a socket and processes them.
func serverLoop(lst *net.UnixListener) {
	for {
		conn, err := lst.AcceptUnix()
		if err != nil {
			log.Fatal(err)
		}
		go processQuery(conn)
	}
}

// spawnMake accepts a socket name and a list of arguments (excluding
// the command name) and spawns our customized version of GNU Make.
func spawnMake(sockName string, argList []string) {
	// Point all calls to make to our customized version.
	os.Setenv("PATH", path.Dir(makeExecutable)+":"+os.Getenv("PATH"))
	os.Setenv("MAKE", makeExecutable)
	os.Setenv("STRESSMAKE_SOCKET", sockName)
	os.Setenv("STRESSMAKE_FAKE_PID", "0")

	// Launch our customized GNU Make and wait for it to complete.
	cmd := exec.Command(makeExecutable, argList...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

// ParseCommandLine parses the command line and returns a list of command-line
// options for GNU Make to parse.
func parseCommandLine() []string {
	// Define our command-line flags.
	fset := flag.NewFlagSet(os.Args[0], flag.ExitOnError)
	fset.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: %s [<stress-make options>] [<make options>] [<targets>]\n", os.Args[0])
		fset.PrintDefaults()
	}
	fset.BoolVar(&verbose, "verbose", false, "Output additional status information")
	qOrder := fset.String("order", "lifo", "Order in which to run processes (\"lifo\", \"fifo\", or \"random\")")
	argNames := []string{"help", "h"}
	fset.VisitAll(func(f *flag.Flag) {
		argNames = append(argNames, f.Name)
	})

	// Split the command line into arguments of interest to stress-make and
	// those of interest to GNU Make.
	ourArgs := make([]string, 0, len(os.Args))
	theirArgs := make([]string, 0, len(os.Args))
argLoop:
	for _, arg := range os.Args[1:] {
		for _, name := range argNames {
			if strings.HasPrefix(arg, "-"+name) || strings.HasPrefix(arg, "--"+name) {
				ourArgs = append(ourArgs, arg)
				continue argLoop
			}
		}
		theirArgs = append(theirArgs, arg)
	}

	// Parse the command line.
	fset.Parse(ourArgs)

	// Abort if we were given a bad option.
	if maxLiveChildren < 1 {
		log.Fatal("At least one process must be allowed to run")
	}
	switch strings.ToLower(*qOrder) {
	case "fifo":
		deqOrder = FIFOOrder
	case "lifo":
		deqOrder = LIFOOrder
	case "random":
		deqOrder = RandomOrder
	default:
		log.Fatalf("Unrecognized execution order %q", *qOrder)
	}
	return theirArgs
}

func main() {
	// Prepare the logger for issuing warnings and errors.
	log.SetFlags(0)
	log.SetPrefix(path.Base(os.Args[0]) + ": ")

	// Parse the command line.
	makeArgs := parseCommandLine()

	// Seed a pseudorandom-number generator.
	seed := int64(os.Getpid()) * int64(os.Getppid()) * int64(time.Now().UnixNano())
	prng = rand.New(rand.NewSource(seed))

	// Allocate a mapping from fake PIDs to commands and from GNU
	// Make fake PIDs to begin times.
	fakePidToCmd = make(map[pid_t]*exec.Cmd)

	// Launch an internal server for serializing accesses to global data
	// structures.
	completedCommands = make(map[pid_t]chan ChildCmd)
	qReqChan = make(chan queueRequest, maxLiveChildren)
	go UpdateState()

	// Create a socket and accept commands from it in the background.
	sockName, listener := createSocket()
	defer os.Remove(sockName)
	go func() {
		// If we receive an exit signal, remove the socket and
		// resend the signal.
		sigChan := make(chan os.Signal, 5)
		signal.Notify(sigChan, os.Interrupt, os.Kill) // Can't really catch Kill.
		sig := <-sigChan
		signal.Stop(sigChan)
		os.Remove(sockName)
		proc, err := os.FindProcess(os.Getpid())
		if err != nil {
			log.Fatal(err)
		}
		proc.Signal(sig)
	}()
	go serverLoop(listener)

	// Run our customized GNU Make as a child process.
	spawnMake(sockName, makeArgs)

	// Report some Makefile statistics.
	stats.Finalize()
	T_1 := stats.GetSequentialTime()
	T_inf := stats.GetParallelTime()
	log.Printf("INFO: Total commands launched = %d", stats.GetTotalProcesses())
	log.Printf("INFO: Maximum concurrency observed = %d", stats.GetMaxConcurrency())
	log.Printf("INFO: Time on a single CPU core (T_1) = %v", T_1)
	log.Printf("INFO: Time on infinite CPU cores (T_inf) = %v", T_inf)
	log.Printf("INFO: Maximum possible speedup (T_1/T_inf) = %f", float64(T_1)/float64(T_inf))
}
