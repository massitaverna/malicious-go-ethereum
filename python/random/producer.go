package main

import "fmt"
import "net"
import "encoding/binary"
import mrand "math/rand"

func main() {
  const (
    ADDR = "unix_socket"
    M = int(7)
  )

  conn, err := net.Dial("unix", ADDR)
  if err != nil {
    fmt.Println(err)
    return
  }
  defer conn.Close()
  
  buf := make([]byte, 4)
  for {

    // Receive the seed
    _, err := conn.Read(buf)
    if err != nil {
      fmt.Println(err)
      return
    }

    seed := binary.BigEndian.Uint32(buf)
    rand := mrand.New(mrand.NewSource(int64(seed)))

    // Receive N
    _, err = conn.Read(buf)
    if err != nil {
      fmt.Println(err)
      return
    }

    N := int(binary.BigEndian.Uint32(buf))

    // Send the random numbers for this seed
    for i := 0; i < N*(2*M+2); i++ {
      num := rand.Intn(100)
      binary.BigEndian.PutUint32(buf, uint32(num))
      conn.Write(buf)
    }
  }
}
