package main

import "fmt"
import "flag"
import "os"
import "os/signal"
import "syscall"
import "github.com/ethereum/go-ethereum/attack/orchestrator"

func main() {
	interruptCh := make(chan struct{})
	handleSigInt(interruptCh)

	port := flag.String("port", "45678", "Specify port to listen on")
	mode := flag.String("mode", "simulation", "Run the attack either on the real or simulated Ethereum world (values: \"real\" or \"simulation\"")
	fraction := flag.Float64("fraction", 0, "Fraction of the total network mining power controlled by the adversary")
	honestHashrate := flag.Float64("honest-hashrate", -1, "The honest, total network mining power (in [H/s]; -1 disables hashrate limiting)")
	flag.Parse()
	errc := make(chan error, 1)				// Channel to signal the first error or success

	orch := orchestrator.New(errc)

	cfg := &orchestrator.OrchConfig{
		Port: *port,
		AtkMode: *mode,
		Fraction: *fraction,
		HonestHashrate: *honestHashrate,
	}
	orch.Start(cfg)
	//orch.Wait()							// Calling Wait() would cause a deadlock if errc was not buffered,
											// because when the attack finishes,
											// the orchestrator wants to send on errc and then quit,
											// but here the main is not receiving on errc until the orch quits.
											// However, at the end of the development, Wait() may not be needed
											// any longer and could be removed from orchestrator.go

	// Wait for something to happen
	select {
	case err := <-errc:						
		if err != nil {
			fmt.Println("Attack failed")
			fmt.Println("Error:", err)
		} else {
			fmt.Println("Attack succeeded")
		}
	case <-interruptCh:
		close(interruptCh)
		orch.Close()
		os.Exit(1)
	}
}


func handleSigInt(interruptCh chan struct{}) {
	sigs := make(chan os.Signal)
	signal.Notify(sigs, syscall.SIGINT)

	go func() {
		<-sigs
		fmt.Println()
		interruptCh <- struct{}{}
	}()
}