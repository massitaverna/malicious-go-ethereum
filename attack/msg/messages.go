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

)

func (m *Message) SetContent(c []byte) *Message {
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
	content := make([]byte, length-1)
	copy(content, messageAsBytes[5:])
	if len(content) != int(length) - 1 {
		return nil
	}
	return &Message{
		Code: code,
		Content: content,
	}
}