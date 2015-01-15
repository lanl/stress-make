/****************************************
 * Send commands to an execution server *
 *                                      *
 * By Scott Pakin <pakin@lanl.gov>      *
 ****************************************/

#include "makeint.h"
#include "filedef.h"
#include "job.h"
#include "commands.h"

#include <sys/types.h>
#include <sys/socket.h>
#include <sys/un.h>
#include <string.h>
#ifndef UNIX_PATH_MAX
# define UNIX_PATH_MAX 108
#endif

#define MAX_REQUEST_LEN 100   /* Maximum bytes in a fixed-length request to the server */
#define MAX_RESPONSE_LEN 100   /* Maximum bytes in a response from the server */

/* Define some global variables. */
char *remote_description = "Stress Tester";  /* Identify ourself to GNU Make */
static int have_server = 0;       /* 1=use stress-test server; 0=ordinary make */
static char *socket_name = NULL;  /* Name of the Unix-domain socket for server communication */
static int fake_pid;              /* Fake PID assigned to us by stress-make */

/* Given a string, escape double quotes and backslashes.  The caller
   should free() the result.  We don't currently handle Unicode
   characters, sorry. */
static char *
quote_for_json (const char *str)
{
  char *newstr = (char *) malloc(strlen(str)*2 + 3);  /* Assume all characters are quoted. */
  const char *c;
  char *nc = newstr;

  for (c = str; *c; c++)
    switch (*c) {
      case '"':
      case '\\':
	*nc++ = '\\';
	*nc++ = *c;
	break;

      case '\n':
	*nc++ = '\\';
	*nc++ = 'n';
	break;
	
      case '\r':
	*nc++ = '\\';
	*nc++ = 'r';
	break;
	
      case '\t':
	*nc++ = '\\';
	*nc++ = 't';
	break;
	
      case '\b':
	*nc++ = '\\';
	*nc++ = 'b';
	break;
	
      case '\f':
	*nc++ = '\\';
	*nc++ = 'f';
	break;

      default:	
	*nc++ = *c;
	break;
    }
  *nc = '\0';
  return newstr;	
}

/* Send a request to the server and receive a response.  Return a
   pointer to a static buffer containing the response or NULL on
   error. */
static const char *
request_response (const char *request)
{
  int local_socket;         /* Socket to use to talk to the server */
  struct sockaddr_un addr;  /* Address on which the stress-test server is listening */
  static char response[MAX_RESPONSE_LEN + 1];   /* Response from the server */

  /* Create a local socket. */
  local_socket = socket(PF_LOCAL, SOCK_STREAM, 0);
  if (local_socket == -1)
    goto failure;

  /* Establish a connection to the server. */
  memset(&addr, 0, sizeof(struct sockaddr_un));
  addr.sun_family = AF_LOCAL;
  strncpy(addr.sun_path, socket_name, UNIX_PATH_MAX - 1);
  if (connect(local_socket, (struct sockaddr *)&addr, sizeof(struct sockaddr_un)) == -1)
    goto failure;

  /* Send the request. */
  if (send(local_socket, request, strlen(request), 0) == -1)
    goto failure;
  if (shutdown(local_socket, SHUT_WR) == -1)
    goto failure;

  /* Prepare a response. */
  memset(response, 0, MAX_RESPONSE_LEN + 1);
  if (recv(local_socket, response, MAX_RESPONSE_LEN, MSG_WAITALL) < 0)
    goto failure;

  /* Return the message. */
  (void) close(local_socket);
  return response;

failure:
  message(1, "Failed to communicate with the stress-testing server");
  have_server = 0;
  return NULL;
}

/* Determine if we were launched by the stress-tester front end. */
void
remote_setup (void)
{
  char *fake_pid_str;   /* Fake PID given us by our parent */
  char *request;        /* Existence announcement to send to stress-make */
  const char *response; /* New fake PID assigned to us by stress-make */

  /* Ensure we were launched by stress-make. */
  socket_name = getenv("STRESSMAKE_SOCKET");
  fake_pid_str = getenv("STRESSMAKE_FAKE_PID");
  if (socket_name == NULL || fake_pid_str == NULL) {
    message(1, "Not performing Makefile stress-testing (not run from stress-make)");
    return;
  }
  socket_name = strdup(socket_name);

  /* Announce our existence to stress-make.  We tell stress-make our
   * parent's fake PID and receive a fake PID of our own in return. */
  request = (char *)malloc(MAX_REQUEST_LEN);
  if (request == NULL) {
    message(1, "Not performing Makefile stress-testing (failed allocation)");
    return;
  }
  sprintf(request, "{\"Request\": \"hello\", \"Pid\": %s}", fake_pid_str);
  response = request_response(request);
  if (response == NULL)
    return;
  free((void *)request);
  fake_pid = atoi(response);

  /* We're ready to go. */
  have_server = 1;
  message(1, "Performing Makefile stress-testing");
}

/* Perform any required cleanup actions. */
void
remote_cleanup (void)
{
}

/* Return nonzero if the next job should be done remotely. */
int
start_remote_job_p (int first_p UNUSED)
{
  return have_server;
}

/* Start a remote job running the command in ARGV,
   with environment from ENVP.  It gets standard input from STDIN_FD.  On
   failure, return nonzero.  On success, return zero, and set *USED_STDIN
   to nonzero if it will actually use STDIN_FD, zero if not, set *ID_PTR to
   a unique identification, and set *IS_REMOTE to zero if the job is local,
   nonzero if it is remote (meaning *ID_PTR is a process ID).  */
int
start_remote_job (char **argv, char **envp, int stdin_fd,
                  int *is_remote, int *id_ptr,
                  int *used_stdin)
{
  char *request = NULL;  /* Message to send */
  char *rp;          /* Pointer into request */
  char *fragment;    /* Fragment of a JSON message to send to the server */
  char **cp;         /* Pointer into argv or envp */
  ssize_t nBytes;    /* Number of bytes to send */
  const char *response;   /* Response from server */

  /* Do nothing if we were not run from the stress-test server. */
  if (!have_server)
    return -1;

  /* Bound the size of the request. */
  nBytes = 100;     /* Fixed contents, rounded up a bit */
  for (cp = argv; *cp; cp++) {
    fragment = quote_for_json(*cp);
    nBytes += strlen(fragment) + 4;    /* String + two double quotes + comma + space */
    free((void *)fragment);    
  }
  for (cp = envp; *cp; cp++) {
    fragment = quote_for_json(*cp);
    nBytes += strlen(fragment) + 4;    /* String + two double quotes + comma + space */
    free((void *)fragment);    
  }
  request = (char *)malloc(nBytes);
  if (request == NULL)
    goto failure;

  /* Construct a job-launch request. */
  rp = request;
  rp += sprintf(rp, "{\"Request\": \"spawn\", \"Args\": [");
  for (cp = argv; *cp; cp++) {
    fragment = quote_for_json(*cp);
    rp += sprintf(rp, "\"%s\", ", fragment);
    free((void *)fragment);
  }
  rp -= 2;  /* Drop the final comma and space. */
  rp += sprintf(rp, "], \"Environ\": [");
  for (cp = envp; *cp; cp++) {
    fragment = quote_for_json(*cp);
    rp += sprintf(rp, "\"%s\", ", fragment);
    free((void *)fragment);
  }
  rp -= 2;  /* Drop the final comma and space. */
  rp += sprintf(rp, "], \"Pid\": %d}", fake_pid);

  /* Send a job-launch request to the stress-test server. */
  response = request_response(request);
  if (response == NULL)
    goto failure;
  *is_remote = 1;
  *id_ptr = atoi(response);
  *used_stdin = 0;
  return 0;

 failure:
  if (request != NULL)
    free((void *)request);
  return -1;
}

/* Get the status of a dead remote child.  Block waiting for one to die
   if BLOCK is nonzero.  Set *EXIT_CODE_PTR to the exit status, *SIGNAL_PTR
   to the termination signal or zero if it exited normally, and *COREDUMP_PTR
   nonzero if it dumped core.  Return the ID of the child that died,
   0 if we would have to block and !BLOCK, or < 0 if there were none.  */
int
remote_status (int *exit_code_ptr, int *signal_ptr,
               int *coredump_ptr, int block)
{
  struct sockaddr_un addr;   /* Address on which the stress-test server is listening */
  char request[MAX_REQUEST_LEN + 1];  /* Message to send or receive */
  const char *response;      /* Response from server */
  int pid;                   /* Process ID to return */

  /* Do nothing if we were not run from the stress-test server. */
  if (!have_server)
    return -1;

  /* Construct a status request. */
  sprintf(request, "{\"Request\": \"status\", \"Block\": %s, \"Pid\": %d}",
	  block ? "true" : "false", fake_pid);

  /* Send a status request to the stress-test server. */
  response = request_response(request);
  if (response == NULL)
    return -1;
  if (sscanf(response, "%d %d %d %d", &pid, exit_code_ptr, signal_ptr, coredump_ptr) != 4)
    return -1;
  return pid;
}

/* Block asynchronous notification of remote child death.
   If this notification is done by raising the child termination
   signal, do not block that signal.  */
void
block_remote_children (void)
{
}

/* Restore asynchronous notification of remote child death.
   If this is done by raising the child termination signal,
   do not unblock that signal.  */
void
unblock_remote_children (void)
{
}

/* Send signal SIG to child ID.  Return 0 if successful, -1 if not.  */
int
remote_kill (int id, UNUSED int sig)
{
  struct sockaddr_un addr;   /* Address on which the stress-test server is listening */
  char request[MAX_REQUEST_LEN]; /* Message to send or receive */
  const char *response;      /* Response from server */
  int pid;          /* Process ID to return */

  /* Do nothing if we were not run from the stress-test server. */
  if (!have_server)
    return -1;

  /* Construct a status request. */
  sprintf(request, "{\"Request\": \"kill\", \"Victim\": %d, \"Pid\": %d}",
	  id, fake_pid);

  /* Send a status request to the stress-test server. */
  response = request_response(request);
  if (response == NULL)
    return -1;
  return atoi(response);
}
