/* This Source Code Form is subject to the terms of the Mozilla Public
 * License, v. 2.0. If a copy of the MPL was not distributed with this
 * file, You can obtain one at http://mozilla.org/MPL/2.0/. */

package process_test

import (
	"context"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/suite"
	"github.com/talos-systems/talos/internal/app/init/pkg/system/events"
	"github.com/talos-systems/talos/internal/app/init/pkg/system/runner"
	"github.com/talos-systems/talos/internal/app/init/pkg/system/runner/process"
	"github.com/talos-systems/talos/internal/app/init/pkg/system/runner/restart"
	"github.com/talos-systems/talos/pkg/userdata"
)

func MockEventSink(state events.ServiceState, message string, args ...interface{}) {
	log.Printf("state %s: %s", state, fmt.Sprintf(message, args...))
}

type ProcessSuite struct {
	suite.Suite

	tmpDir string
}

func (suite *ProcessSuite) SetupSuite() {
	var err error

	suite.tmpDir, err = ioutil.TempDir("", "talos")
	suite.Require().NoError(err)
}

func (suite *ProcessSuite) TearDownSuite() {
	suite.Require().NoError(os.RemoveAll(suite.tmpDir))
}

func (suite *ProcessSuite) TestRunSuccess() {
	r := process.NewRunner(&userdata.UserData{}, &runner.Args{
		ID:          "test",
		ProcessArgs: []string{"/bin/bash", "-c", "exit 0"},
	}, runner.WithLogPath(suite.tmpDir))

	suite.Assert().NoError(r.Open(context.Background()))
	defer func() { suite.Assert().NoError(r.Close()) }()

	suite.Assert().NoError(r.Run(MockEventSink))
	// calling stop when Run has finished is no-op
	suite.Assert().NoError(r.Stop())
}

func (suite *ProcessSuite) TestRunLogs() {
	r := process.NewRunner(&userdata.UserData{}, &runner.Args{
		ID:          "logtest",
		ProcessArgs: []string{"/bin/bash", "-c", "echo -n \"Test 1\nTest 2\n\""},
	}, runner.WithLogPath(suite.tmpDir))

	suite.Assert().NoError(r.Open(context.Background()))
	defer func() { suite.Assert().NoError(r.Close()) }()

	suite.Assert().NoError(r.Run(MockEventSink))

	logFile, err := os.Open(filepath.Join(suite.tmpDir, "logtest.log"))
	suite.Assert().NoError(err)

	// nolint: errcheck
	defer logFile.Close()

	logContents, err := ioutil.ReadAll(logFile)
	suite.Assert().NoError(err)

	suite.Assert().Equal([]byte("Test 1\nTest 2\n"), logContents)
}

func (suite *ProcessSuite) TestRunRestartFailed() {
	testFile := filepath.Join(suite.tmpDir, "talos-test")
	// nolint: errcheck
	_ = os.Remove(testFile)

	r := restart.New(process.NewRunner(&userdata.UserData{}, &runner.Args{
		ID:          "restarter",
		ProcessArgs: []string{"/bin/bash", "-c", "echo \"ran\"; test -f " + testFile},
	}, runner.WithLogPath(suite.tmpDir)), restart.WithType(restart.UntilSuccess), restart.WithRestartInterval(time.Millisecond))

	suite.Assert().NoError(r.Open(context.Background()))
	defer func() { suite.Assert().NoError(r.Close()) }()

	var wg sync.WaitGroup

	wg.Add(1)
	go func() {
		defer wg.Done()
		suite.Assert().NoError(r.Run(MockEventSink))
	}()

	time.Sleep(200 * time.Millisecond)

	f, err := os.Create(testFile)
	suite.Assert().NoError(err)
	suite.Assert().NoError(f.Close())

	wg.Wait()

	logFile, err := os.Open(filepath.Join(suite.tmpDir, "restarter.log"))
	suite.Assert().NoError(err)
	// nolint: errcheck
	defer logFile.Close()

	logContents, err := ioutil.ReadAll(logFile)
	suite.Assert().NoError(err)

	suite.Assert().True(len(logContents) > 20)
}

func (suite *ProcessSuite) TestStopFailingAndRestarting() {
	testFile := filepath.Join(suite.tmpDir, "talos-test")
	// nolint: errcheck
	_ = os.Remove(testFile)

	r := restart.New(process.NewRunner(&userdata.UserData{}, &runner.Args{
		ID:          "endless",
		ProcessArgs: []string{"/bin/bash", "-c", "test -f " + testFile},
	}, runner.WithLogPath(suite.tmpDir)), restart.WithType(restart.Forever), restart.WithRestartInterval(5*time.Millisecond))

	suite.Assert().NoError(r.Open(context.Background()))
	defer func() { suite.Assert().NoError(r.Close()) }()

	done := make(chan error, 1)

	go func() {
		done <- r.Run(MockEventSink)
	}()

	time.Sleep(40 * time.Millisecond)

	select {
	case <-done:
		suite.Assert().Fail("task should be running")
		return
	default:
	}

	f, err := os.Create(testFile)
	suite.Assert().NoError(err)
	suite.Assert().NoError(f.Close())

	time.Sleep(40 * time.Millisecond)

	select {
	case <-done:
		suite.Assert().Fail("task should be running")
		return
	default:
	}

	suite.Assert().NoError(r.Stop())
	<-done
}

func (suite *ProcessSuite) TestStopSigKill() {
	r := process.NewRunner(&userdata.UserData{}, &runner.Args{
		ID:          "nokill",
		ProcessArgs: []string{"/bin/bash", "-c", "trap -- '' SIGTERM; while :; do :; done"},
	},
		runner.WithLogPath(suite.tmpDir),
		runner.WithGracefulShutdownTimeout(10*time.Millisecond),
	)

	suite.Assert().NoError(r.Open(context.Background()))
	defer func() { suite.Assert().NoError(r.Close()) }()

	done := make(chan error, 1)

	go func() {
		done <- r.Run(MockEventSink)
	}()

	time.Sleep(100 * time.Millisecond)

	suite.Assert().NoError(r.Stop())
	<-done
}

func TestProcessSuite(t *testing.T) {
	suite.Run(t, new(ProcessSuite))
}
