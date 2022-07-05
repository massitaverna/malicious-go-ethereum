package main

import "fmt"
import "flag"
import "os"
import "os/signal"
import "syscall"
import "github.com/ethereum/go-ethereum/attack/orchestrator"

func main() {
	handleSigInt()

	rebuild := flag.Bool("rebuild", false, "Rebuild underlying chain(s) and overwrite them in buildchain tool's directory")
	port    := flag.String("port", "45678", "Specify port to listen on")
	predictionOnly := flag.Bool("prediction-only", false, "Quit after leaking bitstring")
	shortPrediction := flag.Bool("short-prediction", false, "Only leak 3 bits (and assume seed is 1). Useful for testing purposes")
	overrideSeed := flag.Int("override-seed", -1, "Override leaked seed with specified one. Negative values do not override it")
	flag.Parse()
	errc := make(chan error, 1)				// Channel to signal the first error or success

	orch := orchestrator.New(errc)
	orch.Start(*rebuild, *port, *predictionOnly, *shortPrediction, *overrideSeed)
	//orch.Wait()							// Calling Wait() would cause a deadlock if errc was not buffered,
											// because when the attack finishes,
											// the orchestrator wants to send on errc and then quit,
											// but here the main is not receiving on errc until the orch quits.
											// However, at the end of the development, Wait() may not be needed
											// any longer and could be removed from orchestrator.go

	err := <-errc							// Wait for something to happen
	if err != nil {
		fmt.Println("Attack failed")
		fmt.Println("Error:", err)
	} else {
		fmt.Println("Attack succeeded")
	}
}


func handleSigInt() {
	sigs := make(chan os.Signal)
	signal.Notify(sigs, syscall.SIGINT)

	go func() {
		<-sigs
		fmt.Println()
		os.Exit(1)
	}()
}