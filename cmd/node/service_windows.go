//go:build windows

package main

import (
	"context"
	"log"

	"golang.org/x/sys/windows/svc"
)

// isServiceSession reports whether the service control manager started this
// process, as opposed to someone typing it in a console.
//
// A console-launched node must not call StartServiceCtrlDispatcher: it would
// fail, and the node would refuse to start from the terminal.
func isServiceSession() bool {
	inService, err := svc.IsWindowsService()
	if err != nil {
		// Deciding false on error is deliberate. A node that is really a service
		// but cannot tell will come up without a dispatcher and SCM will time it
		// out with a far less obvious error than this one, and a console run must
		// keep working either way.
		log.Printf("windows service: cannot determine session type, assuming console: %v", err)
		return false
	}
	return inService
}

// runAsWindowsService connects to the service control manager and blocks until
// SCM stops the service. It returns only when the service is done.
func runAsWindowsService() error {
	log.Printf("starting as Windows service %q", serviceName)
	return svc.Run(serviceName, &nodeService{})
}

// nodeService adapts the service control manager to runNode.
//
// It exists because runNode only knows how to stop when its context is
// cancelled. SCM speaks a request/response protocol instead, so this type owns
// the bridge: a Stop or Shutdown request cancels the context, and the handler
// does not return until runNode has finished shutting down.
type nodeService struct{}

func (s *nodeService) Execute(args []string, r <-chan svc.ChangeRequest, changes chan<- svc.Status) (bool, uint32) {
	const accepted = svc.AcceptStop | svc.AcceptShutdown

	changes <- svc.Status{State: svc.StartPending}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// runNode blocks until ctx is cancelled and then flushes and closes the
	// store, so it runs beside the request loop rather than instead of it.
	done := make(chan struct{})
	go func() {
		defer close(done)
		runNode(ctx)
	}()

	changes <- svc.Status{State: svc.Running, Accepts: accepted}
	log.Printf("service %q is running", serviceName)

	for {
		select {
		case c := <-r:
			switch c.Cmd {
			case svc.Interrogate:
				// SCM asks for current status; c already carries it.
				changes <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				changes <- svc.Status{State: svc.StopPending}
				log.Printf("service %q received %v, flushing and closing...", serviceName, c.Cmd)
				cancel()
				// Wait for the node to finish. Returning here makes svc.Run return
				// and this process exit, which would abandon the store flush and
				// leave every listener closed only by the process dying.
				<-done
				log.Printf("service %q stopped", serviceName)
				return false, uint32(accepted)
			default:
				// Not accepted, so SCM should never send these. Say so rather
				// than dropping the request silently.
				log.Printf("service %q: ignoring unsupported control %d", serviceName, c.Cmd)
			}
		case <-done:
			// runNode returned without a Stop request: a fatal path inside the
			// node ended it. Reporting Stopped and exiting cleanly is the honest
			// outcome; the recovery actions on the service handle the restart.
			log.Printf("service %q exited on its own", serviceName)
			return false, uint32(accepted)
		}
	}
}
