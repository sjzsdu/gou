package v8

import (
	"fmt"
	"time"

	"github.com/yaoapp/gou/runtime/v8/bridge"
	"github.com/yaoapp/kun/log"
	"rogchap.com/v8go"
)

// Runner is the v8 runner
type Runner struct {
	id        uint
	iso       *v8go.Isolate
	ctx       *v8go.Context
	status    uint8
	signal    chan uint8
	chResp    chan interface{}
	keepalive bool
	script    *Script
	method    string
	sid       string
	args      []interface{}
	global    map[string]interface{}
}

var seq uint = 0

const (
	// RunnerStatusInit is the runner status init
	RunnerStatusInit uint8 = iota

	// RunnerStatusRunning is the runner status running
	RunnerStatusRunning

	// RunnerStatusCleaning is the runner status cleaning
	RunnerStatusCleaning

	// RunnerStatusReady is the runner status ready
	RunnerStatusReady

	// RunnerStatusDestroy is the runner status destroy
	RunnerStatusDestroy

	// RunnerCommandDestroy is the runner command destroy
	RunnerCommandDestroy

	// RunnerCommandClean is the runner command clean
	RunnerCommandClean

	// RunnerCommandReset is the runner command reset
	RunnerCommandReset

	// RunnerCommandExec is the runner command exec
	RunnerCommandExec

	// RunnerCommandClose is the runner command close
	RunnerCommandClose

	// RunnerCommandStatus is the runner command status
	RunnerCommandStatus
)

// NewRunner create a new v8 runner
func NewRunner(keepalive bool) *Runner {
	seq++
	return &Runner{
		id:        seq,
		iso:       nil,
		ctx:       nil,
		signal:    make(chan uint8, 2),
		keepalive: keepalive,
		status:    RunnerStatusInit,
	}
}

// Start start the v8 runner
func (runner *Runner) Start(ready chan bool) error {

	// Set the status to free
	if runner.status != RunnerStatusInit {
		err := fmt.Errorf("[runner] you can't start a runner with status: [%d]", runner.status)
		log.Error(err.Error())
		return err
	}

	iso := v8go.YaoNewIsolate()
	tmpl := MakeTemplate(iso)
	ctx := v8go.NewContext(iso, tmpl)
	runner.iso = iso
	runner.ctx = ctx

	runner.status = RunnerStatusReady
	if runner.keepalive {
		dispatcher.online(runner)
	}

	ticker := time.NewTicker(time.Millisecond * 50)
	ready <- true

	// Command loop
	for {
		select {
		case <-ticker.C:
			break

		case signal := <-runner.signal:
			switch signal {

			case RunnerCommandClean:
				runner.clean()
				break

			case RunnerCommandReset:
				runner.reset()
				break

			case RunnerCommandExec:
				runner.exec()
				break

			case RunnerCommandClose:
				runner.close()
				break

			case RunnerCommandDestroy:
				runner.destory()
				return nil

			default:
				log.Warn("runner unknown signal: %d", signal)
			}

		}
	}
}

// Destroy send a destroy signal to the v8 runner
func (runner *Runner) Destroy(cb func()) {
	runner.signal <- RunnerCommandDestroy
}

// Reset send a reset signal to the v8 runner
func (runner *Runner) Reset(cb func()) {
	runner.signal <- RunnerCommandReset
}

// Exec send a script to the v8 runner to execute
func (runner *Runner) Exec(script *Script) interface{} {

	runner.status = RunnerStatusRunning
	runner.script = script
	runner.chResp = make(chan interface{})
	log.Debug(fmt.Sprintln("2.  Exec a script to the v8 runner to execute", "runner.id:", runner.id, "status:", runner.status, "keepalive:", runner.keepalive, len(runner.signal)))

	runner.signal <- RunnerCommandExec
	select {
	case res := <-runner.chResp:
		return res
	}
}

// Context get the context
func (runner *Runner) Context() (*v8go.Context, error) {
	return runner.ctx, nil
}

func (runner *Runner) exec() {

	defer func() {
		go func() {
			if !runner.keepalive {
				log.Debug(fmt.Sprintln("3.1 Send a destory signal to the v8 runner", "runner.id:", runner.id, "status:", runner.status, runner.keepalive))
				runner.signal <- RunnerCommandDestroy
				log.Debug(fmt.Sprintln("3.2 Send a destory signal to the v8 runner done", "runner.id:", runner.id, "status:", runner.status))
				return
			}
			runner.signal <- RunnerCommandClose
			log.Debug(fmt.Sprintln("3.  Send a close signal to the v8 runner done", "runner.id:", runner.id, "status:", runner.status, runner.keepalive))
		}()
	}()

	// runner.chResp <- "OK"
	runner._exec()
}

func (runner *Runner) _exec() {

	// Create instance of the script
	instance, err := runner.iso.CompileUnboundScript(runner.script.Source, runner.script.File, v8go.CompileOptions{})
	if err != nil {
		runner.chResp <- err
		return
	}
	v, err := instance.Run(runner.ctx)
	if err != nil {
		return
	}
	defer v.Release()

	// Set the global data
	global := runner.ctx.Global()
	err = bridge.SetShareData(runner.ctx, global, &bridge.Share{
		Sid:    runner.sid,
		Root:   runner.script.Root,
		Global: runner.global,
	})
	if err != nil {
		runner.chResp <- err
		return
	}

	// Run the method
	jsArgs, err := bridge.JsValues(runner.ctx, runner.args)
	if err != nil {
		runner.chResp <- err
		return

	}
	defer bridge.FreeJsValues(jsArgs)

	jsRes, err := global.MethodCall(runner.method, bridge.Valuers(jsArgs)...)
	if err != nil {
		runner.chResp <- err
		return
	}

	goRes, err := bridge.GoValue(jsRes, runner.ctx)
	if err != nil {
		runner.chResp <- err
		return
	}

	runner.chResp <- goRes
}

func (runner *Runner) close() {

	log.Debug(fmt.Sprintln("4.  close the runner", "runner.id:", runner.id, "status:", runner.status))
	log.Debug(fmt.Sprintf("--- %d end -----------------\n\n", runner.id))

	if runner.keepalive {
		runner.reset()
		return
	}

	// Clean the runner
	if runner.signal != nil {
		close(runner.signal)
	}
	runner.ctx.Close()
	runner.iso.Dispose()
	runner.iso = nil
	runner.ctx = nil
	runner.args = nil
}

// destory the runner
func (runner *Runner) destory() {

	log.Debug(fmt.Sprintln("4.  destory the runner", "runner.id:", runner.id, "status:", runner.status))
	log.Debug(fmt.Sprintf("--- %d end -----------------\n\n", runner.id))

	runner.status = RunnerStatusDestroy
	if runner.signal != nil {
		close(runner.signal)
	}

	runner.ctx.Close()
	runner.iso.Dispose()
	runner.iso = nil
	runner.ctx = nil
	runner = nil
}

// reset the runner
func (runner *Runner) reset() {

	runner.status = RunnerStatusDestroy
	// dispatcher.UpdateStatus(runner, RunnerStatusDestroy)

	runner.ctx.Close()
	runner.iso.Dispose()
	runner.iso = v8go.YaoNewIsolate()
	runner.ctx = v8go.NewContext(runner.iso, MakeTemplate(runner.iso))
	// log.Info("[runner] reset the runner: [%p]", runner)

	// Set the status to free
	runner.status = RunnerStatusReady
	dispatcher.online(runner)

}

func (runner *Runner) clean() {
	runner.status = RunnerStatusCleaning
	runner.ctx.Close()
	runner.ctx = v8go.NewContext(runner.iso, MakeTemplate(runner.iso))
	// log.Info("[runner] clean the runner: [%p]", runner)

	// Set the status to free
	runner.status = RunnerStatusReady
	dispatcher.online(runner)

}
