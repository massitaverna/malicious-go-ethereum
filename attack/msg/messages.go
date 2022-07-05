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
	MustDisconnectVictim = &Message{
		Code: 10,
		Content: []byte{0},
	}
	SolicitMustDisconnectVictim = &Message{
		Code: 11,
		Content: nil,
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