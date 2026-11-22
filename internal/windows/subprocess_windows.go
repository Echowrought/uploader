package windows

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"
	"unsafe"

	win "golang.org/x/sys/windows"
)

// The tray owns the only, non-inheritable job handle. Closing it, including on
// process termination, kills every owned subprocess. The mutex protects creation
// against a concurrent Close; it is never held while waiting for a child to finish.
type subprocessJob struct {
	mu     sync.Mutex
	handle win.Handle
}

func newSubprocessJob() (*subprocessJob, error) {
	handle, err := win.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	var limits win.JOBOBJECT_EXTENDED_LIMIT_INFORMATION
	limits.BasicLimitInformation.LimitFlags = win.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := win.SetInformationJobObject(handle, win.JobObjectExtendedLimitInformation, uintptr(unsafe.Pointer(&limits)), uint32(unsafe.Sizeof(limits))); err != nil {
		win.CloseHandle(handle)
		return nil, err
	}
	return &subprocessJob{handle: handle}, nil
}

func (job *subprocessJob) Close() error {
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.handle == 0 {
		return nil
	}
	err := win.CloseHandle(job.handle)
	job.handle = 0
	return err
}

func (job *subprocessJob) output(args []string, timeout time.Duration) ([]byte, error) {
	stdout, childStdout, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer stdout.Close()
	defer childStdout.Close()
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	defer null.Close()
	process, err := job.start(args, win.Handle(childStdout.Fd()), win.Handle(null.Fd()))
	if err != nil {
		return nil, err
	}
	defer win.CloseHandle(process)
	childStdout.Close()

	// Drain stdout concurrently with waiting so output cannot fill the pipe
	// and prevent the child from exiting. The timeout bounds one command.
	finished := make(chan error, 1)
	go func() { finished <- waitSubprocess(process, timeout) }()
	output, readErr := io.ReadAll(stdout)
	if readErr != nil {
		win.TerminateProcess(process, 1)
	}
	waitErr := <-finished
	return output, errors.Join(readErr, waitErr)
}

func (job *subprocessJob) start(args []string, stdout, null win.Handle) (win.Handle, error) {
	job.mu.Lock()
	defer job.mu.Unlock()
	if job.handle == 0 {
		return 0, os.ErrClosed
	}
	application, err := win.UTF16PtrFromString(args[0])
	if err != nil {
		return 0, err
	}
	command, err := win.UTF16PtrFromString(win.ComposeCommandLine(args))
	if err != nil {
		return 0, err
	}
	attributes, err := win.NewProcThreadAttributeList(2)
	if err != nil {
		return 0, err
	}
	defer attributes.Delete()

	// Windows 10+ assigns the job during creation, avoiding a start/assign gap.
	const procThreadAttributeJobList = 0x0002000D
	if err := attributes.Update(procThreadAttributeJobList, unsafe.Pointer(&job.handle), unsafe.Sizeof(job.handle)); err != nil {
		return 0, err
	}
	handles := []win.Handle{stdout, null}
	for _, handle := range handles {
		if err := win.SetHandleInformation(handle, win.HANDLE_FLAG_INHERIT, win.HANDLE_FLAG_INHERIT); err != nil {
			return 0, err
		}
	}
	if err := attributes.Update(win.PROC_THREAD_ATTRIBUTE_HANDLE_LIST, unsafe.Pointer(&handles[0]), uintptr(len(handles))*unsafe.Sizeof(handles[0])); err != nil {
		return 0, err
	}
	var startup win.StartupInfoEx
	startup.Cb = uint32(unsafe.Sizeof(startup))
	startup.Flags = win.STARTF_USESTDHANDLES
	startup.StdInput, startup.StdOutput, startup.StdErr = null, stdout, null
	startup.ProcThreadAttributeList = attributes.List()
	var process win.ProcessInformation
	if err := win.CreateProcess(application, command, nil, nil, true, win.EXTENDED_STARTUPINFO_PRESENT|win.CREATE_NO_WINDOW, nil, nil, &startup.StartupInfo, &process); err != nil {
		return 0, err
	}
	win.CloseHandle(process.Thread)
	return process.Process, nil
}

func waitSubprocess(process win.Handle, timeout time.Duration) error {
	status, err := win.WaitForSingleObject(process, uint32(timeout/time.Millisecond))
	if err != nil || status == uint32(win.WAIT_TIMEOUT) {
		// Always reap a terminated child before its process handle is closed.
		killErr := win.TerminateProcess(process, 1)
		_, waitErr := win.WaitForSingleObject(process, win.INFINITE)
		if err == nil {
			err = errors.New("subprocess timed out")
		}
		return errors.Join(err, killErr, waitErr)
	}
	var code uint32
	if err := win.GetExitCodeProcess(process, &code); err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("subprocess exited with code %d", code)
	}
	return nil
}
