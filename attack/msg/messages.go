package msg

import "encoding/binary"

type Message struct {
	Code byte
	Content []byte
}

var (
	ServeLastFullBatch = &Message{
		Code: 0,
		Content: nil,
	}
	BatchRequestServed = &Message{
		Code: 1,
		Content: []byte{0, 0, 0, 0},
	}
	NextPhase = &Message{
		Code: 2,
		Content: nil,
	}
	SetCheatAboutTd = &Message{
		Code: 3,
		Content: []byte{0},
	}
	SetNumBatches = &Message{
		Code: 4,
		Content: []byte{0, 0, 0, 0},
	}
	OracleBit = &Message{
		Code: 5,
		Content: []byte{0},
	}
	MasterPeer = &Message{
		Code: 6,
		Content: nil,
	}
	SetVictim = &Message{
		Code: 7,
		Content: []byte("00000000"),
	}
	NewMaliciousPeer = &Message{
		Code: 8,
		Content: []byte("00000000"),
	}
	SetAttackPhase = &Message{
		Code: 9,
		Content: []byte{0},
	}
	Rollback = &Message{
		Code: 10,
		Content: nil,
	}
	AnnouncedSyncTd = &Message{
		Code: 11,
		Content: []byte{0},
	}
	Terminate = &Message{
		Code: 12,
		Content: []byte{0},
	}
	AvoidVictim = &Message{
		Code: 13,
		Content: []byte{0},
	}
	LastOracleBit = &Message{
		Code: 14,
		Content: nil,
	}
	TerminatingStateSync = &Message{
		Code: 15,
		Content: nil,
	}
	InfoPRNG = &Message{
		Code: 16,
		Content: []byte("00000000"),
	}
	WithholdInit = &Message{
		Code: 17,
		Content: []byte{0, 0, 0, 0},
	}
	WithholdACK = &Message{
		Code: 18,
		Content: []byte{0},
	}
	Release = &Message{
		Code: 19,
		Content: []byte{0, 0, 0, 0},
	}
	WithholdReset = &Message{
		Code: 20,
		Content: nil,
	}
	CommitTrieRoot = &Message{
		Code: 21,
		Content: []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0},
	}
	AssignedRange = &Message{
		Code: 22,
		Content: []byte{0},
	}
	CompletedRange = &Message{
		Code: 23,
		Content: []byte{0},
	}
	ResetRangeInfo = &Message{
		Code: 24,
		Content: []byte{0},
	}
	LastRangeQuery = &Message{
		Code: 25,
		Content: []byte{0},
	}
	LastRangeQueryACK = &Message{
		Code: 26,
		Content: []byte{0},
	}
	OriginalHead = &Message{
		Code: 27,
		Content: []byte{0},
	}
	GetCwd = &Message{
		Code: 28,
		Content: nil,
	}
	Cwd = &Message{
		Code: 29,
		Content: []byte{0},
	}
	FakeBatch = &Message{
		Code: 30,
		Content: []byte{0},
	}
	GhostRoot = &Message{
		Code: 31,
		Content: []byte{0},
	}
	EndOfAttack = &Message{
		Code: 32,
		Content: nil,
	}
)

func (m *Message) SetContent(c []byte) *Message {
	if c == nil {
		return &Message{
			Code: m.Code,
			Content: nil,
		}
	}

	contentCopy := make([]byte, len(c))
	copy(contentCopy, c)
	return &Message{
		Code: m.Code,
		Content: contentCopy,
	}
}

func (m *Message) Encode() []byte {
	result := make([]byte, 4)
	length := uint32(len(m.Content) + 1)				// m.Code takes 1 byte
	binary.BigEndian.PutUint32(result, length)
	result = append(result, m.Code)
	result = append(result, m.Content...)
	return result
}

func Decode(messageAsBytes []byte) *Message {
	length := binary.BigEndian.Uint32(messageAsBytes[:4])
	code := messageAsBytes[4]
	var content []byte
	if int(length) - 1 != 0 {
		content = make([]byte, length-1)
		copy(content, messageAsBytes[5:])
		if len(content) != int(length) - 1 {
			return nil
		}
	}
	return &Message{
		Code: code,
		Content: content,
	}
}