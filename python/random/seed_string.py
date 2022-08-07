import os
import sys
from socket import *
import subprocess


#
# Constants
#
ADDR = './unix_socket'
M = 7
N = 51                       # Length of the bitstring
int32max = (1 << 31) - 1     # k-bit seed --> (1 << k) - 1


#
# Functions
#

def sequenceToBinString(s):
  res = ""
  for n in s:
    if n < 50:
      res += "0"
    else:
      res += "1"
  return res

def binStringToBytes(binStr):
  numBytes = ceil(len(binStr)/8)
  return (int(binStr, 2)).to_bytes(numBytes, 'big')
  
 
def openConnection():
  s = socket(AF_UNIX, SOCK_STREAM)
  os.unlink(ADDR)
  s.bind(ADDR)
  s.listen(1)

  subprocess.Popen(['go', 'run', 'producer.go'])

  conn, _ = s.accept()
  print("Go Producer started")

  return conn
  
def sendSeedAndN(seed, N, conn):
  conn.send(seed.to_bytes(4, 'big'))
  conn.send(N.to_bytes(4, 'big'))
  
def recvNum(conn):
  numAsBytes = conn.recv(4)
  num = (int).from_bytes(numAsBytes, 'big')
  return num



def main():
  # Connect to Go for randomness generation
  conn = openConnection()
  
  # Initialize the operation for this seed
  if len(sys.argv) < 2:
    print("Pass seed as argument")
    sys.exit()
  seed = int(sys.argv[1])
  seed = seed % int32max
  sendSeedAndN(seed, N, conn)
  sequence = list()
  
  # Collect N random numbers
  for i in range(N):
    # Skip first 2*M random numbers
    for _ in range(2*M):
      _ = recvNum(conn)
    # Keep (2*M + 1)-th number
    num = recvNum(conn)
    sequence.append(num)

    # Skip (2*M+2)-th number
    _ = recvNum(conn)

  binStr = sequenceToBinString(sequence)
  print('bitstring:', binStr)
      
  conn.close()
  
  
if __name__ == '__main__':
  main()

  
  
  
