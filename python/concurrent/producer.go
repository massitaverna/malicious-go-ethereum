package main

import "fmt"
import "net"
import "os"
import "encoding/binary"
import mrand "math/rand"

func main() {
  const (
    ADDR = "unix_socket"
    M = int(7)
  )

  sockName := os.Args[1]
  conn, err := net.Dial("unix", sockName)
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
    if N == 0 {     // Use N==0 as sentinel value to stop the producer
      return
    }

    // Send the random numbers for this seed
    for i := 0; i < N*(2*M+2); i++ {
      num := rand.Intn(100)
      binary.BigEndian.PutUint32(buf, uint32(num))
      conn.Write(buf)
    }
  }
}
