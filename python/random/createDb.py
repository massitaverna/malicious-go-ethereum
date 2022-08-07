import os
from socket import *
from math import ceil
import subprocess
import plyvel


#
# Constants
#
ADDR = './unix_socket'
PARAMS_FILE = 'params.txt'
DB_DIRECTORY = './seeds/'
N_START = 51
M = 7
int32max = (1 << 31) - 1


#
# Locally-defined exceptions
#
class KeyExistsException(Exception):
  pass


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
  
def readParams():
  global N_START
  if not os.path.exists(PARAMS_FILE):
    f = open(PARAMS_FILE, 'w')
    f.write(f"0\n{N_START}\n")
    f.close()
    return 0, N_START
  else:
    f = open(PARAMS_FILE, 'r')
    lastProcessedSeed = int(f.readline()[:-1])
    n = int(f.readline()[:-1])
    f.close()
    return lastProcessedSeed, n
    
def writeParams(seed, n):
  if seed < 0:
    seed = 0

  f = open(PARAMS_FILE, 'w')
  f.write(str(seed)+'\n'+str(n)+'\n')
  f.close()
  print("Seed =", seed, "and N =", n, "written to file")
 
def openConnection():
  s = socket(AF_UNIX, SOCK_STREAM)
  try:
    os.unlink(ADDR)
  except FileNotFoundError:
    pass
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
  # Open (or create) the database and load last fully processed seed
  db = plyvel.DB(DB_DIRECTORY, create_if_missing=True)
  wb = db.write_batch()
  cacheCtr = 0

  lastProcessedSeed, N = readParams()
  
  # Connect to Go for randomness generation
  conn = openConnection()
  
  complete = False
  while not complete:
    try:
      print("Starting with seed", lastProcessedSeed+1, "and N =", N)
      
      # We skip int32max because it gets reduced to 0.
      # We also skip 0 because Go internally switches 0 to 89482311 (approx. 1.3 * 2^26, seems a random value...)
      for seed in range(lastProcessedSeed+1, int32max):
        key = None
        
        # Print progress
        if seed != 0 and (seed % (int32max//100) == 0):
          print(seed//(int32max//100), "\b%... ", end='', flush=True)

        # Initialize the operation for this seed
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
        #print("Produced binary string:", binStr)
        key = binStringToBytes(binStr)
        #print("Encoded as:", key)
        value = seed.to_bytes(4, 'big')
      
        if db.get(key) != None and db.get(key) != value:
          print("Seed", seed, "collides with seed", (int).from_bytes(db.get(key), 'big'))
          raise KeyExistsException
   
        db.put(key, value)
        lastProcessedSeed = seed
        lastSavedKey = key

      complete = True
        
    except KeyExistsException:
      N += 1
      lastProcessedSeed = 0
      db.close()
      plyvel.destroy_db(DB_DIRECTORY)
      db = plyvel.DB(DB_DIRECTORY, create_if_missing=True)
      continue

    
    except KeyboardInterrupt:
      # Saving the seed preceding the last processed one for safety
      writeParams(lastProcessedSeed-1, N)

      db.close()
      conn.close()

      print("Program stopped by user.")
      return
    

  db.close()
  conn.close()
  
  
if __name__ == '__main__':
  main()

  
  
  
