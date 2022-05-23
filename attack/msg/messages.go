package msg

var (
	PeerDropped = byte(0)
	GotNewBatchRequest = byte(1)
	NextPhase = byte(2)
	EnableCheatAboutTd = byte(3)
	DisableCheatAboutTd = byte(4)
)