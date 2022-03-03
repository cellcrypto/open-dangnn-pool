package hook

import (
	"fmt"
	"os"
	"os/signal"
	"reflect"
	"sync"
	"syscall"
	"time"
)

type ShutdownHook struct {
	hooks     map[string]func(os.Signal)
	hooksMain func(os.Signal)
	mutex     *sync.Mutex
}

var defaultHook = &ShutdownHook{ mutex: &sync.Mutex{},hooks: map[string]func(os.Signal){}}

func Listen(signals ...os.Signal) {
	defaultHook.Listen(signals...)
}


func RegistryHook(name string, fn func(string)) {
	defaultHook.RegistryHook(name, fn)
}

func RegistryMainHook(fn func()) {
	defaultHook.RegistryMainHook(fn)
}

func (s *ShutdownHook) RegistryMainHook(fn func()) {
	s.RegistryMainHookWithParam(func(os.Signal) {
		fmt.Printf("[####] main shutdown process start...\n")
		fn()
		fmt.Printf("[####] main shutdown process end...\n")
	})
}


func (s *ShutdownHook) RegistryHook(name string, fn func(string)) {
	s.RegistryHookWithParam(name, func(os.Signal) {
		fmt.Printf("[####] %v shutdown process start...\n", name)
		fn(name)
		fmt.Printf("[####] %v shutdown process end...\n", name)
	})
}


func (s *ShutdownHook) RegistryHookWithParam(name string, fn func(os.Signal)) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	s.hooks[name] = fn
}

func (s *ShutdownHook) RegistryMainHookWithParam( fn func(os.Signal)) {
	s.hooksMain = fn
}


func (s *ShutdownHook) Hooks() map[string]func(os.Signal) {
	s.mutex.Lock()
	defer s.mutex.Unlock()
	fns := map[string]func(os.Signal){}
	for k, v := range s.hooks {
		fns[k] = v
	}
	return fns
}

func (s *ShutdownHook) Listen(signals ...os.Signal) {
	ch := make(chan os.Signal, 1)

	var (
		sig os.Signal
		timeGap int64
	)

	for {
		signal.Notify(ch, signals...)
		sig = <-ch
		target := reflect.ValueOf(sig)
		fmt.Println("[######] Enter End Signal... ", target.Type(), sig.String())
		if sig == syscall.SIGINT {
			timeNow := time.Now().UnixNano()
			if timeNow < timeGap+(1000*int64(time.Millisecond)) {
				break
			}
			timeGap = time.Now().UnixNano()
		} else if sig == syscall.SIGTERM || sig == syscall.SIGKILL {
			break
		}
	}

	fmt.Println("[######] shutdown process start... ", sig.String())

	// SUB HOOK PROCESS
	var wg sync.WaitGroup
	for _, fn := range s.Hooks() {
		wg.Add(1)
		go func(sig os.Signal, fn func(os.Signal)) {
			defer wg.Done()
			fn(sig)
		}(sig, fn)
	}

	wg.Wait()

	// MAIN HOOK PROCESS
	if s.hooksMain != nil {
		wg.Add(1)
		go func(sig os.Signal, fn func(os.Signal)) {
			defer wg.Done()
			fn(sig)
		}(sig, s.hooksMain)
	}

	wg.Wait()

	fmt.Println("[######] shutdown process complete...")
}

