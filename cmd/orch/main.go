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

	rebuild := flag.Bool("rebuild", false, "Rebuild underlying chain(s) and overwrite them in buildchain tool's directory")
	port    := flag.String("port", "45678", "Specify port to listen on")
	predictionOnly := flag.Bool("prediction-only", false, "Quit after leaking bitstring")
	shortPrediction := flag.Bool("short-prediction", false, "Only leak 3 bits (and assume seed is 1). Useful for testing purposes")
	overrideSeed := flag.Int("override-seed", -1, "Override leaked seed with specified one. Negative values do not override it")
	mode := flag.String("mode", "simulation", "Run the attack either on the real or simulated Ethereum world (values: \"real\" or \"simulation\"")
	Tm := flag.Int("time", -1, "Time for mining the fake segment")
	fraction := flag.Float64("fraction", 0, "Fraction of the total network mining power controlled by the adversary")
	honestHashrate := flag.Float64("honest-hashrate", 65536, "The honest, total network mining power (in [H/s])")
	ghostAttack := flag.Bool("ghost", false, "Runs a SNaP-Ghost attack insead of a standard SNaP attack")
	flag.Parse()
	errc := make(chan error, 1)				// Channel to signal the first error or success

	orch := orchestrator.New(errc)

	cfg := &orchestrator.OrchConfig{
		Rebuild: *rebuild,
		Port: *port,
		PredictionOnly: *predictionOnly,
		ShortPrediction: *shortPrediction,
		OverriddenSeed: *overrideSeed,
		AtkMode: *mode,
		Tm: *Tm,
		Fraction: *fraction,
		HonestHashrate: *honestHashrate,
		GhostAttack: *ghostAttack,
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