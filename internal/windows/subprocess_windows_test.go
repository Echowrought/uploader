package windows

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"

	win "golang.org/x/sys/windows"
)

func TestSubprocessJobCapturesOutputLargerThanPipeBuffer(t *testing.T) {
	job := fixtureSubprocessJob(t)
	args := subprocessChildArgs(t, "TestSubprocessOutputChild")

	output, err := job.output(args, 5*time.Second)

	want := strings.Repeat("x", 64<<10) + "\u00a0"
	if err != nil || string(output) != want {
		t.Fatalf("output length=%d, want=%d, error=%v", len(output), len(want), err)
	}
}

func TestSubprocessJobTimeoutTerminatesChild(t *testing.T) {
	job := fixtureSubprocessJob(t)
	args := subprocessChildArgs(t, "TestSubprocessWaitingChild")

	_, err := job.output(args, 100*time.Millisecond)

	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("unexpected timeout result: %v", err)
	}
}

func TestClosedSubprocessJobRejectsNewChildren(t *testing.T) {
	job := fixtureSubprocessJob(t)
	args := subprocessChildArgs(t, "TestSubprocessWaitingChild")

	closeErr := job.Close()
	_, startErr := job.output(args, time.Second)
	secondCloseErr := job.Close()

	if closeErr != nil || secondCloseErr != nil || startErr == nil {
		t.Fatalf("close=%v, start=%v, second close=%v", closeErr, startErr, secondCloseErr)
	}
}

func TestClosingSubprocessJobTerminatesRunningChild(t *testing.T) {
	job := fixtureSubprocessJob(t)
	args := subprocessChildArgs(t, "TestSubprocessWaitingChild")
	stdout, null := subprocessNullHandles(t)
	process, err := job.start(args, stdout, null)
	if err != nil {
		t.Fatal(err)
	}
	defer win.CloseHandle(process)

	before, beforeErr := win.WaitForSingleObject(process, 0)
	closeErr := job.Close()
	after, afterErr := win.WaitForSingleObject(process, 5000)

	if beforeErr != nil || before != uint32(win.WAIT_TIMEOUT) {
		t.Fatalf("child was not running: status=%d, error=%v", before, beforeErr)
	}
	if closeErr != nil || afterErr != nil || after != win.WAIT_OBJECT_0 {
		t.Fatalf("child survived job closure: close=%v, status=%d, wait=%v", closeErr, after, afterErr)
	}
}

func TestSubprocessDiesWhenOwnerIsForciblyTerminated(t *testing.T) {
	args := subprocessChildArgs(t, "TestSubprocessOwnerChild")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	owner := exec.CommandContext(ctx, args[0], args[1:]...)
	var stderr bytes.Buffer
	owner.Stderr = &stderr
	stdout, err := owner.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := owner.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() {
		owner.Process.Kill()
		owner.Wait()
	}()
	line, err := bufio.NewReader(stdout).ReadString('\n')
	if err != nil {
		owner.Process.Kill()
		owner.Wait()
		t.Fatalf("owner did not report a child: %v", err)
	}
	pid, err := strconv.ParseUint(strings.TrimSpace(line), 10, 32)
	if err != nil {
		t.Fatal(err)
	}
	child, err := win.OpenProcess(win.SYNCHRONIZE|win.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer win.CloseHandle(child)

	killErr := owner.Process.Kill()
	ownerErr := owner.Wait()
	status, waitErr := win.WaitForSingleObject(child, 5000)

	if killErr != nil || ownerErr == nil || waitErr != nil || status != win.WAIT_OBJECT_0 {
		t.Fatalf("child survived owner termination: kill=%v, owner=%v, child status=%d, wait=%v, stderr=%s", killErr, ownerErr, status, waitErr, stderr.String())
	}
}

// These helpers run only in headless child copies of this test executable.
func TestSubprocessOutputChild(t *testing.T) {
	if os.Args[len(os.Args)-1] != "subprocess-child" {
		t.Skip("child process only")
	}
	_, _ = io.WriteString(os.Stdout, strings.Repeat("x", 64<<10)+"\u00a0")
	os.Exit(0)
}

func TestSubprocessWaitingChild(t *testing.T) {
	if os.Args[len(os.Args)-1] != "subprocess-child" {
		t.Skip("child process only")
	}
	_, _ = win.WaitForSingleObject(win.CurrentProcess(), win.INFINITE)
	os.Exit(2)
}

func TestSubprocessOwnerChild(t *testing.T) {
	if os.Args[len(os.Args)-1] != "subprocess-child" {
		t.Skip("child process only")
	}
	job := fixtureSubprocessJob(t)
	stdout, null := subprocessNullHandles(t)
	child, err := job.start(subprocessChildArgs(t, "TestSubprocessWaitingChild"), stdout, null)
	if err != nil {
		t.Fatal(err)
	}
	defer win.CloseHandle(child)
	pid, err := win.GetProcessId(child)
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprintln(os.Stdout, pid)
	_, _ = win.WaitForSingleObject(win.CurrentProcess(), win.INFINITE)
	os.Exit(2)
}

func subprocessNullHandles(t *testing.T) (win.Handle, win.Handle) {
	t.Helper()
	stdout, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { stdout.Close() })
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { null.Close() })
	return win.Handle(stdout.Fd()), win.Handle(null.Fd())
}

func fixtureSubprocessJob(t *testing.T) *subprocessJob {
	t.Helper()
	job, err := newSubprocessJob()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { job.Close() })
	return job
}

func subprocessChildArgs(t *testing.T, name string) []string {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return []string{executable, "-test.run=^" + name + "$", "--", "subprocess-child"}
}
