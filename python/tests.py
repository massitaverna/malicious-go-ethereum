import os
import sys
import subprocess
import time
from random import randint
from threading import Thread, Lock
from concurrent.futures import ThreadPoolExecutor
import traceback

GETH_START_PORT = 30300
ORCH_START_PORT = 45678

N_TESTS_WITHOUT_HONEST = 15
N_TESTS_WITH_HONEST = 30
CONCURRENT_TESTS_WITHOUT_HONEST = 15
CONCURRENT_TESTS_WITH_HONEST = 6

HONEST_PEERS = 4
MALICIOUS_PEERS = 2
TESTNET_DIR = os.path.expanduser('~/tests_testnet/')
GENESIS_FILE = TESTNET_DIR + 'genesis.json'
TRUE_CHAIN_FILE = TESTNET_DIR + 'truechain'
BITSTRING_CALCULATOR = TESTNET_DIR + 'seed_string.py'

PREDICTION_CHAIN_LENGTH = 8*192 + 88

cwdLock = Lock()
unixSocketLock = Lock()

class Bootnode:
	def __init__(self, directory, address):
		self.dir = directory
		self.address = address

class Test:
	def __init__(self, n, gethPort, orchPort, withHonest, results):
		self.n = n
		self.dir = None
		self.gethPort = gethPort
		self.orchPort = orchPort
		self.withHonest = withHonest
		self.results = results
		self.bootnode = None
		self.startTime = None
		self.orchProc = None
		self.orchOutput = None
		self.honestNodesProcs = list()
		self.victimProc = None
		self.nodesFiles = list()
		self.logFile = None
		self.terminated = False
		self.stateLock = Lock()

		self.seed = randint(0, (1 << 63) - 1)
		self.bitstring = None
		int32max = (1 << 31) - 1
		unixSocketLock.acquire()
		bitstringOutput = subprocess.run([sys.executable, BITSTRING_CALCULATOR, str(self.seed % int32max)],
										capture_output=True, text=True
									   ).stdout
		unixSocketLock.release()
		for line in bitstringOutput.split('\n'):
			if 'bitstring' in line:
				self.bitstring = line.split()[1]
				break

	def run(self):
		cwdLock.acquire()			# As these functions call os.chdir(), we lock this code section
									# so that one thread won't mess the cwd for another thread
		self.createDir()
		self.log("Seed:", self.seed)
		self.log("Honest peers: " + ("yes" if self.withHonest else "no"))
		self.logFile.flush()
		self.initNodes()

		if self.withHonest:
			self.startHonestNodes()
		self.startMaliciousPeers()
		cwdLock.release()

		time.sleep(90) # Let the network boot up before the victim joins it
		self.startVictim()

		self.startTime = time.time()
		print("Test no.", self.n, "started")

		Thread(target=self.checkProgress).start()
		self.orchProc.wait()		# Note: when the attack will go beyond the prediction phase, the orchestrator
									# will exit later, so if we want to test only prediction in this test suite,
									# we need to either detect end of prediction in another way, or make the
									# orchestrator exit at the end of the prediction (maybe with a flag, e.g.
									# 'orch --only-prediction' )
		self.stateLock.acquire()
		if self.terminated:
			self.stateLock.release()
			return
		self.terminated = True
		self.stateLock.release()

		endTime = time.time()
		self.orchOutput.close()

		cwdLock.acquire()
		bitstring = self.getBitstring()
		cwdLock.release()

		if bitstring != self.bitstring:
			self.fail(f'expected {self.bitstring}, got {bitstring}')
			return

		self.log("Success")
		self.log("Start time:", self.startTime)
		self.log("End time:", endTime)
		duration = int(endTime - self.startTime)
		stats(duration, self.n)
		self.log("Duration:", duration//60, "\bm", duration%60, "\bs")
		self.close()
		self.results[self.n] = True

	def createDir(self):
		self.dir = TESTNET_DIR + f'test{self.n}/'
		os.mkdir(self.dir)
		self.logFile = open(self.dir + 'test.log', 'w')

	def initNodes(self):
		if self.withHonest:
			for i in range(HONEST_PEERS):
				nodeDir = self.dir + f'node{i+1}/'
				os.mkdir(nodeDir)
				os.chdir(nodeDir)
				os.mkdir(nodeDir + 'datadir/')
				subprocess.run(['geth', '--datadir', 'datadir', 'init', GENESIS_FILE], stderr=subprocess.DEVNULL)
				subprocess.run(['geth', '--datadir', 'datadir', 'import', TRUE_CHAIN_FILE], stdout=subprocess.DEVNULL,
								stderr=subprocess.DEVNULL)

				if self.bootnode is None:
					pubkey = subprocess.run(['bootnode', '-nodekey', './datadir/geth/nodekey', '-writeaddress'],
											capture_output=True
											).stdout.decode('ascii').strip()
					nodeAddress = f'enode://{pubkey}@127.0.0.1:0?discport={self.gethPort+i+1}'
					self.bootnode = Bootnode(nodeDir, nodeAddress)

				os.chdir(TESTNET_DIR)

		for i in range(MALICIOUS_PEERS):
			nodeDir = self.dir + f'peer{i+1}/'
			os.mkdir(nodeDir)
			os.chdir(nodeDir)
			os.mkdir(nodeDir + 'datadir/')
			subprocess.run(['geth', '--datadir', 'datadir', 'init', GENESIS_FILE], stderr=subprocess.DEVNULL)
			subprocess.run(['geth', '--datadir', 'datadir', 'import', TRUE_CHAIN_FILE], stdout=subprocess.DEVNULL,
							stderr=subprocess.DEVNULL)

			if self.bootnode is None:
				pubkey = subprocess.run(['bootnode', '-nodekey', './datadir/geth/nodekey', '-writeaddress'],
										capture_output=True
										).stdout.decode('ascii').strip()
				nodeAddress = f'enode://{pubkey}@127.0.0.1:0?discport={self.gethPort+i+1}'
				self.bootnode = Bootnode(nodeDir, nodeAddress)

			os.chdir(TESTNET_DIR)

		nodeDir = self.dir + 'victim/'
		os.mkdir(nodeDir)
		os.chdir(nodeDir)
		os.mkdir(nodeDir + 'datadir/')
		subprocess.run(['geth', '--datadir', 'datadir', 'init', GENESIS_FILE], stderr=subprocess.DEVNULL)
		os.chdir(TESTNET_DIR)


	def startMaliciousPeers(self):
		os.chdir(self.dir)
		self.orchOutput = open('orch.log', 'w')
		self.orchProc = subprocess.Popen(['orch', '-port', str(self.orchPort)],
										 stdout=self.orchOutput, stderr=subprocess.STDOUT)
		time.sleep(0.5) # Let orch open its socket for peers

		portOffset = 0
		if self.withHonest:
			portOffset = HONEST_PEERS

		for i in range(MALICIOUS_PEERS):
			os.chdir(self.dir + f'peer{i+1}/')
			logF = open('peer.log', 'w')
			self.nodesFiles.append(logF)
			subprocess.Popen(['mgeth', '--datadir', 'datadir', '--port', str(self.gethPort+i+1+portOffset),
							  '--bootnodes', self.bootnode.address, '--orchport', str(self.orchPort),
							  '--discovery.dns', ''],
							 stdout=logF, stderr=subprocess.STDOUT)

		os.chdir(TESTNET_DIR)

	def startHonestNodes(self):
		minerSet = False	# To switch honest mining on/off, flip this variable. False means mining will happen.
		for i in range(HONEST_PEERS):
			nodeDir = self.dir + f'node{i+1}/'
			os.chdir(nodeDir)
			logF = open('node.log', 'w')
			self.nodesFiles.append(logF)

			p = None
			if self.bootnode.dir == nodeDir:
				p = subprocess.Popen(['geth', '--datadir', 'datadir', '--port', str(self.gethPort+i+1),
									  '--discovery.dns', ''],
									 stdout=logF, stderr=subprocess.STDOUT)
			elif not minerSet:
				p = subprocess.Popen(['geth', '--datadir', 'datadir', '--port', str(self.gethPort+i+1),
									  '--bootnodes', self.bootnode.address, '--mine', '--miner.threads', '4',
									  '--miner.etherbase', '0x0000000000000000000000000000000000000001',
									  '--discovery.dns', ''],
									 stdout=logF, stderr=subprocess.STDOUT)
				minerSet = True
			else:
				p = subprocess.Popen(['geth', '--datadir', 'datadir', '--port', str(self.gethPort+i+1),
									  '--bootnodes', self.bootnode.address, '--discovery.dns', ''],
									 stdout=logF, stderr=subprocess.STDOUT)

			self.honestNodesProcs.append(p)

		os.chdir(TESTNET_DIR)

	def startVictim(self):
		#os.chdir(self.dir + 'victim/')
		victimDatadir = self.dir + 'victim/datadir/'
		logF = open(self.dir + 'victim/victim.log', 'w')
		self.nodesFiles.append(logF)

		victimPort = self.gethPort + MALICIOUS_PEERS + (HONEST_PEERS if self.withHonest else 0) + 1
		self.victimProc = subprocess.Popen(['geth', '--datadir', victimDatadir, '--port', str(victimPort),
											'--bootnodes', self.bootnode.address, '--seed', str(self.seed),
											'--discovery.dns', ''],
										   stdout=logF, stderr=subprocess.STDOUT)

		#os.chdir(TESTNET_DIR)

	def checkProgress(self):
		start = self.orchOutput.tell()
		time.sleep(2*60)

		self.stateLock.acquire()
		while not self.terminated:
			end = self.orchOutput.tell()
			if start == end:
				self.fail(f'Attack stalling')
				return
			if end - self.startTime > 60*30: # == half an hour
				self.fail('Attack was taking too long')
			start = end
			self.stateLock.release()
			time.sleep(2*60)
			self.stateLock.acquire()

	def getBitstring(self):
		os.chdir(self.dir)
		bitstring = None
		with open('orch.log', 'r') as f:
			for line in f:
				if "bitstring" in line:
					bitstring = line.partition('[')[2].strip()[:-1].replace(' ', '')
					break
		os.chdir(TESTNET_DIR)
		return bitstring

	def killHonestNodes(self):
		for p in self.honestNodesProcs:
			p.kill()

	def fail(self, s):
		self.log("Fail")
		self.log("Reason:", s)
		end = time.time()
		duration = int(end - self.startTime)
		stats(duration, self.n)
		self.log("Start time:", self.startTime)
		self.log("End time:", end)
		self.log("Duration:", duration//60, "\bm", duration%60, "\bs")
		self.close()
		self.results[self.n] = False

	def close(self):
		self.stateLock.acquire()
		self.orchProc.kill()
		self.orchOutput.close()
		self.killHonestNodes()
		self.victimProc.kill()
		self.logFile.close()
		for f in self.nodesFiles:
			f.close()
		self.terminated = True
		self.stateLock.release()

	def log(self, *s):
		print(*s, file=self.logFile)

	def setupBootnode(self):
		bootnodeDir = self.dir + 'bootnode/'
		os.mkdir(bootnodeDir)
		os.chdir(bootnodeDir)
		os.system('bootnode -genkey boot.key')
		os.system('bootnode -nodekey boot.key -writeaddress > pubkey.txt')
		with open('pubkey.txt', 'r') as f:
			pubkey = f.read().strip()
		#os.system('rm pubkey.txt')
		bootnodeAddress = f'enode://{pubkey}@127.0.0.1:0?discport={gethPort}'
		self.bootnode = Bootnode(bootnodeDir, bootnodeAddress)


results = []
maxDuration = 0
maxDurTestNum = -1

def stats(dur, testNum):
	global maxDuration
	global maxDurTestNum

	if dur > maxDuration:
		maxDuration = dur
		maxDurTestNum = testNum

def runTest(t):
	global results
	try:
		t.run()
	except Exception:
		print("Exception in test no.", t.n)
		traceback.print_exc()

	print(f"Test no. {t.n} finished (outcome:", 'success)' if results[t.n] else 'fail)')

def printAndWrite(*s, to=None):
	print(*s)
	print(*s, file=to)

def main():
	global results
	global maxDuration
	global maxDurTestNum

	os.chdir(TESTNET_DIR)

	print("Turning off network interface")
	os.system('sudo ifconfig enp0s3 down')

	print("Resetting tests directory")
	for i in range(1, 1 + N_TESTS_WITHOUT_HONEST+N_TESTS_WITH_HONEST):
		os.system(f'rm -rf test{i}')

	# Build prediction chain as we need it for the tests
	os.system(f'buildchain -n {PREDICTION_CHAIN_LENGTH} -type prediction -overwrite')


	results = [None for _ in range(1 + N_TESTS_WITHOUT_HONEST + N_TESTS_WITH_HONEST)]

	tests = list()
	for i in range(1, 1 + N_TESTS_WITHOUT_HONEST):
		nTest = i
		gethPort = GETH_START_PORT + 10*nTest
		orchPort = ORCH_START_PORT + nTest
		tests.append(Test(nTest, gethPort, orchPort, False, results))
	print("Starting tests without honest peers")
	with ThreadPoolExecutor(max_workers=CONCURRENT_TESTS_WITHOUT_HONEST) as executor:
		try:
			for result in executor.map(runTest, tests):	# This loop is needed just to prevent the executor
														# from suppressing threads' exceptions.
														# Useful for debugging purposes.
				pass
		except Exception:
			traceback.print_exc()

	tests = list()
	for i in range(1, 1 + N_TESTS_WITH_HONEST):
		nTest = i + N_TESTS_WITHOUT_HONEST
		gethPort = GETH_START_PORT + 10*nTest
		orchPort = ORCH_START_PORT + nTest
		tests.append(Test(nTest, gethPort, orchPort, True, results))
	print("Starting tests with honest peers")
	with ThreadPoolExecutor(max_workers=CONCURRENT_TESTS_WITH_HONEST) as executor:
		try:
			for result in executor.map(runTest, tests):	# This loop is needed just to prevent the executor
														# from suppressing threads' exceptions.
														# Useful for debugging purposes.
				pass
		except Exception:
			traceback.print_exc()

	outcomeFile = open(TESTNET_DIR + 'outcome.txt')
	numFailed = 0
	for i, r in enumerate(results):
		if i == 0:
			continue

		if r is None:
			print(f"Error: test {i} should be finished at this point")
		if not r:
			numFailed += 1
			printAndWrite("Test", i, "failed", to=outcomeFile)

	if numFailed == 0:
		printAndWrite("All tests were successful", to=outcomeFile)
	else:
		printAndWrite("Total failed tests:", numFailed, to=outcomeFile)
	printAndWrite("Total run tests:", N_TESTS_WITHOUT_HONEST + N_TESTS_WITH_HONEST,
		 f"({N_TESTS_WITHOUT_HONEST}+{N_TESTS_WITH_HONEST})", to=outcomeFile)
	printAndWrite("Max. duration:", maxDuration//60, "\bm", maxDuration%60, "\bs", f"(test no. {maxDurTestNum})",
				  to=outcomeFile)

	outcomeFile.close()



if __name__ == '__main__':
	main()
	