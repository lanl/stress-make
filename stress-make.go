// Run a process-creation server and point our customized GNU Make at it.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path"
	"strconv"
	"sync/atomic"
	"syscall"
	"time"
)

// makeExecutable is the full path to our customized GNU Make.
const makeExecutable = "/tmp/gnumake/bin/make"

// Define a dequeue order.
type DequeueOrder int

// Define the order that we run enqueued processes.
const (
	FIFOOrder   = DequeueOrder(iota) // First in, first out
	LIFOOrder                        // Last in, first out
	RandomOrder                      // Random order
)

// deqOrder specifies the order in which we dequeue commands to run.
var deqOrder DequeueOrder = LIFOOrder

// maxLiveChildren specifies the maximum number of children we allow
// to execute concurrently.
var maxLiveChildren int64 = 1

// currentLiveChildren specifies the number of children that are
// currently executing.  The initial GNU Make process counts as 1.
var currentLiveChildren int64 = 1

// maxConcurrencyObserved keeps track of the maximum concurrency the Makefile
// can take advantage of.
var maxConcurrencyObserved int64 = 0

// totalEnqueued keeps track of the total number of children enqueued.
var totalEnqueued int64 = 0

// ChildCmd extends *exec.Cmd with a fake PID to return to GNU Make.
type ChildCmd struct {
	*exec.Cmd
	FakePid uint64
}

// completedCommands reports children that have completed on a per-PID
// basis (i.e., PID of the GNU Make process requesting the child).
var completedCommands map[uint64]chan ChildCmd

// fakePidToCmd maps a fabricated process ID to the associated command
// structure.
var fakePidToCmd map[uint64]*exec.Cmd

// maxPid is the maximum process ID allowed by the OS.
var maxPid uint64 = 32768

// nextPid is the next available (fake) process ID we should return.
var nextFakePid uint64 = 2

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

// readProcUint64 reads a uint64 from Linux's proc filesystem.
func readProcUint64(procFile string) (value uint64, err error) {
	var fd *os.File
	fd, err = os.Open(procFile)
	if err != nil {
		return
	}
	defer fd.Close()
	sizeStr := make([]byte, 25)
	nRead, err := fd.Read(sizeStr)
	if err != nil {
		return
	}
	value, err = strconv.ParseUint(string(sizeStr[:nRead]), 10, 64)
	return
}

// A queueRequest is used internally to request updates to global data
// structures.  MakePid and RespChan must always be specified.  If neither
// Command nor Killpid are specified, this requests that a pending shell
// command be run.
type queueRequest struct {
	MakePid  uint64      // Process ID (from a GNU Make process) that initiated the request
	Command  *exec.Cmd   // Command to enqueue on the pending queue
	KillPid  uint64      // (Fake) process ID to kill
	RespChan chan uint64 // Response (success code or PID)
}

// qReqChan is the channel used to talk to the single instance of UpdateState.
var qReqChan chan queueRequest

// UpdateState safely updates global state in a concurrent context.  Much of
// what the program actually does is encapsulated in the UpdateState routine.
func UpdateState() {
	allPendingCommands := make([]uint64, 0, maxLiveChildren) // All pending commands (by fake PID) across all Make processes
	pidPendingCommands := make(map[uint64]map[uint64]empty)  // Subset of allPendingCommands local to a Make PID
	for qReq := range qReqChan {
		if qReq.RespChan == nil {
			log.Fatal("Internal error processing a queue request")
		}
		mkPid := qReq.MakePid
		switch {
		case qReq.Command != nil:
			// Allocate a new fake PID.
			fPid := nextFakePid
			fakePidToCmd[fPid] = qReq.Command
			if nextFakePid == maxPid {
				nextFakePid = 2
			} else {
				nextFakePid++
			}

			// Enqueue a new pending command on both the global
			// list and the per-PID set.
			allPendingCommands = append(allPendingCommands, fPid)
			pidMap, ok := pidPendingCommands[mkPid]
			if !ok {
				// This is the first request from PID MakePid.
				pidMap = make(map[uint64]empty, maxLiveChildren)
				pidPendingCommands[mkPid] = pidMap
			}
			pidMap[fPid] = *&empty{}

			// For reporting purposes, keep track of the maximium
			// number of pending + running commands that existed at
			// any one time and of the total number of commands
			// enqueued.
			nConc := int64(len(allPendingCommands)) + atomic.LoadInt64(&currentLiveChildren) - 1 // -1 because of GNU Make itself
			if nConc > maxConcurrencyObserved {
				maxConcurrencyObserved = nConc
			}
			totalEnqueued++

			// Return the fake PID.
			qReq.RespChan <- fPid
			close(qReq.RespChan)

		case qReq.KillPid > 0:
			// Kill the job if it's running.
			fPid := qReq.KillPid
			cmd, ok := fakePidToCmd[fPid]
			if !ok {
				// Error case (invalid fake PID)
				qReq.RespChan <- 0
				close(qReq.RespChan)
				continue
			}
			if cmd.Process != nil {
				// Job has launched.  Kill it.
				if cmd.Process.Signal(os.Kill) != nil {
					ok = false
				}
			}

			// Whether or not the job has launched, elide the
			// command from allPendingCommands, and delete it from
			// pidPendingCommands.  Also delete the association
			// between the fake PID and the command.
			delete(fakePidToCmd, fPid)
			for i := range allPendingCommands {
				if allPendingCommands[i] == fPid {
					allPendingCommands = append(allPendingCommands[:i], allPendingCommands[i+1:]...)
					delete(pidPendingCommands[mkPid], fPid)
					break
				}
			}
			if ok {
				qReq.RespChan <- 1
			} else {
				qReq.RespChan <- 0
			}
			close(qReq.RespChan)
			continue

		default:
			// Ensure we have a command to run.
			nPending := len(allPendingCommands)
			if nPending == 0 {
				if atomic.LoadInt64(&currentLiveChildren) == 0 {
					// Something is seriously wrong.  Make
					// thought it had pending commands, but
					// it didn't.
					qReq.RespChan <- 0
				} else {
					// We have nothing new to run so the
					// caller just needs to wait for the
					// currently running commands to
					// finish.
					qReq.RespChan <- 1
				}
				close(qReq.RespChan)
				continue
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

				// Elide the command from allPendingCommands,
				// and delete it from pidPendingCommands.
				fPid := allPendingCommands[idx]
				cmd := fakePidToCmd[fPid]
				allPendingCommands = append(allPendingCommands[:idx], allPendingCommands[idx+1:]...)
				delete(pidPendingCommands[mkPid], fPid)
				nPending--

				// Create a completion queue if necessary.
				if _, ok := completedCommands[mkPid]; !ok {
					completedCommands[mkPid] = make(chan ChildCmd, maxLiveChildren)
				}

				// Run the command in the background.
				atomic.AddInt64(&currentLiveChildren, 1)
				go func() {
					// Run the command then "kill" it to
					// remove it from fakePidToCmd.
					err := cmd.Run()
					if err != nil {
						log.Fatal(err)
					}
					atomic.AddInt64(&currentLiveChildren, -1)
					qRespChan := make(chan uint64)
					qReqChan <- queueRequest{MakePid: mkPid, KillPid: fPid, RespChan: qRespChan}
					_ = <-qRespChan
					completedCommands[mkPid] <- ChildCmd{Cmd: cmd, FakePid: fPid}
				}()
			}
			qReq.RespChan <- 1
			close(qReq.RespChan)
		}
	}
}

// A RemoteQuery represents a query received from a socket.
type RemoteQuery struct {
	Request string   // Either "spawn", "status", or "kill"
	Args    []string // spawn: Command to run plus all arguments
	Environ []string // spawn: Environment to use (list of key=value pairs)
	Block   bool     // status: true=wait; false=return immediately
	Victim  uint64   // kill: Victim process ID (really a fake PID)
	Pid     uint64   // all: GNU Make process ID
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
		if keyval[:4] == "PWD=" {
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
	qRespChan := make(chan uint64)
	qReqChan <- queueRequest{MakePid: query.Pid, Command: cmd, RespChan: qRespChan}
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

	// If GNU Make blocks, it no longer counts as live.
	if query.Block {
		atomic.AddInt64(&currentLiveChildren, -1)
		defer atomic.AddInt64(&currentLiveChildren, 1)
	}

	// Launch as many new commands as possible,
	qRespChan := make(chan uint64)
	qReqChan <- queueRequest{MakePid: mkPid, RespChan: qRespChan}
	if <-qRespChan == 0 {
		// Something went wrong -- probably that there are no new
		// commands to run.  Notify the caller.
		fmt.Fprint(conn, "-1 0 0 0")
		return
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
	qRespChan := make(chan uint64)
	qReqChan <- queueRequest{MakePid: mkPid, KillPid: fPid, RespChan: qRespChan}
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

	// Launch our customized GNU Make and wait for it to complete.
	cmd := exec.Command(makeExecutable, argList...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		log.Fatal(err)
	}
}

func main() {
	// Prepare the logger for issuing warnings and errors.
	log.SetFlags(0)
	log.SetPrefix(os.Args[0] + ": ")

	// Seed a pseudorandom-number generator.
	seed := int64(os.Getpid()) * int64(os.Getppid()) * int64(time.Now().UnixNano())
	prng = rand.New(rand.NewSource(seed))

	// Determine the maximum process ID we're allowed to use.
	var err error
	maxPid, err = readProcUint64("/proc/sys/kernel/pid_max")
	if err != nil {
		maxPid = 32768
	}
	fakePidToCmd = make(map[uint64]*exec.Cmd)

	// Launch an internal server for serializing accesses to global data
	// structures.
	completedCommands = make(map[uint64]chan ChildCmd)
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
	spawnMake(sockName, os.Args[1:])

	// Report some Makefile statistics.
	log.Printf("INFO: Total commands launched = %d", totalEnqueued)
	log.Printf("INFO: Maximum concurrency observed = %d", maxConcurrencyObserved)
}
