import os
import time
from random import randint
from socket import *
from math import ceil
import queue as _queue
import subprocess
import multiprocessing as mp
import psutil
import plyvel


#
# Data generation section
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
  
def openConnection(sockName):
  s = socket(AF_UNIX, SOCK_STREAM)
  try:
    os.unlink(sockName)
  except FileNotFoundError:
    pass
  s.bind(sockName)
  s.listen(1)

  subprocess.Popen(['go', 'run', 'producer.go', sockName])

  conn, _ = s.accept()

  return conn

def closeConnection(conn):
  sendSeedAndN(0, 0, conn)
  conn.close()
  
def sendSeedAndN(seed, N, conn):
  conn.send(seed.to_bytes(4, 'big'))
  conn.send(N.to_bytes(4, 'big'))
  
def recvNum(conn):
  numAsBytes = conn.recv(4)
  num = (int).from_bytes(numAsBytes, 'big')
  return num

  
#
# Multiprocessing section
#

def main():
  N = 62
  nWorkers = 32
  tableSize = 2**31
  step = tableSize // nWorkers
  done = False

  psutil.Process().cpu_affinity([0,56])

  while not done:
    print('Start with N =', N)
    queue = mp.Queue()
    successQueue = mp.Queue()
    exitEvents = list()
    workers = list()

    for w in range(nWorkers):
      start = w*step
      end = (w+1)*step
      if start == 0:
        start = 1
      if end == tableSize:
        end = tableSize-1

      ee = mp.Event()
      exitEvents.append(ee)
      p = mp.Process(target=worker, args=(start, end, N, queue, ee,))
      p.start()
      while p.pid is None:
        pass
      psutil.Process(p.pid).cpu_affinity([w+1,w+57])
      workers.append(p)

    mgr = mp.Process(target=manager, args=(nWorkers, tableSize, queue, successQueue, exitEvents,))
    mgr.start()
    while mgr.pid is None:
      pass
    psutil.Process(mgr.pid).cpu_affinity([0,56])

    startTime = time.time()
    done = successQueue.get()
    endTime = time.time()
    print(f'Duration: {int(endTime-startTime)}s')

    if not done:
      N += 1
      print('About to restart...')
      canRestart = False
      while not canRestart:
        canRestart = True
        for p in workers:
          if p.is_alive():
            print('Some processes still alive, waiting...')
            canRestart = False
            time.sleep(0.5)
            break
      for p in workers:
        p.close()

  print(f'Found N = {N} and stored DB to disk')
  os.system('rm unix_socket* 2> /dev/null')


def manager(nWorkers, tableSize, queue, successQueue, exitEvents):
  finished = 0
  table = dict()
  seedsNum = 0
  progress = 0

  while finished < nWorkers:
    L, ok = queue.get()

    if not ok:
      stopWorkers(queue, exitEvents)
      successQueue.put(False)
      return

    if L is None:
      finished += 1
      continue

    seedsNum += len(L)
    for (k, v) in L:
      if k in table and table[k] != v:
        print(f'Collision: {int.from_bytes(v, "big")} <--> {int.from_bytes(table[k], "big")}')
        stopWorkers(queue, exitEvents)
        successQueue.put(False)
        return
      else:
        table[k] = v

    progressStep = 2**10
    if seedsNum // (tableSize // progressStep) > progress:
      progress = seedsNum // (tableSize // progressStep)
      print(f'Progress (computation): {progress}/{progressStep}', flush=True)

  print('Seeds processed:', seedsNum)
  stopWorkers(queue, exitEvents)

  # Table built. Now, save it to DB.
  if len(table) != tableSize - 2:
    print(f'Warning: table misses some values: have {len(table)} != {tableSize-2} want')
  if len(table) > tableSize:
    print('Error: table has too many values')
    _ = input("Press ENTER to continue")
    successQueue.put(False)
    return

  writeDivisor = 8
  step = tableSize//writeDivisor
  numDone = 0
  progress = 0
  db = plyvel.DB('../seeds_table', create_if_missing=True)
  wb = None

  for key in table:
    if numDone % step == 0:
      wb = db.write_batch()
    wb.put(key, table[key])
    numDone += 1
    if numDone % step == 0:
      wb.write()
    if numDone // (tableSize // progressStep) > progress:
      progress = numDone // (tableSize // progressStep)
      print(f'Progress (write): {progress}/{progressStep}', flush=True)

  if numDone % step != 0:
    wb.write()

  db.close()
  successQueue.put(True)

def stopWorkers(queue, exitEvents):
  for ee in exitEvents:
    ee.set()

  empty = False
  timeout = 10
  while not empty:
    try:
      queue.get(True, timeout)
    except _queue.Empty:
      empty = True

  os.system('rm unix_socket_* 2> /dev/null')

def worker(startSeed, endSeed, N, queue, exitEvent):
  M = 7
  pid = os.getpid()
  table = dict()
  cache = list()
  cacheThreshold = 2**15

  #time.sleep(randint(1,1)) # To prevent processes from accessing the queue
                            # all at the same time (i.e. to reduce lock time)


  # Connect to Go for randomness generation
  conn = openConnection(f'unix_socket_{pid}')
    
  for seed in range(startSeed, endSeed):
    # Check if worker must quit
    if exitEvent.is_set():
      quitWorker(conn, table, cache)
      return

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
    key = binStringToBytes(binStr)
    value = seed.to_bytes(4, 'big')
  
    '''
    if key in table and value != table[key]:
      print(f'Collision: {seed} <--> {int.from_bytes(table[key], "big")}')
      queue.put((None, False))
      quitWorker(conn, table, cache)
      return
    '''

    #table[key] = value
    cache.append((key, value))

    if len(cache) > cacheThreshold:
      queue.put((cache[:], True))
      cache.clear()

  queue.put((cache[:], True))
  queue.put((None, True))
  quitWorker(conn, table, cache)
  return

def quitWorker(conn, table, cache):
  closeConnection(conn)
  table.clear()
  cache.clear()
  
  
  
if __name__ == '__main__':
  main()
