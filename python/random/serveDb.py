from socket import *
from threading import Thread
import sys
import plyvel

IP = 'localhost'
PORT = 65432
DB_PATH = 'seeds_table_62/'
BITSTRING_LEN = 8		# In bytes


def serveClient(sock):
	key = sock.recv(BITSTRING_LEN)
	seed = query(key)
	sock.sendall(seed)

	sock.close()

def query(key):
	global db
	minusOne = b'\xff\xff\xff\xff'
	if not isinstance(key, bytes):
		return minusOne

	while len(key) < BITSTRING_LEN:
		key = b'\x00' + key

	seed = db.get(key)
	if seed is None:
		print("Seed not found in database. Sending -1.")
		print("Bitstring:", key)
		return minusOne

	if len(seed) != 4:
		print("Warning: seed stored in table is not 4 bytes long. Sending -1.")
		print("Seed:", seed)
		print("Bitstring:", key)
		return minusOne

	print("Seed found. Serving it.")
	print("Seed:", int.from_bytes(seed, "big"))
	return seed



def main():
	global db
	global DB_PATH
	if len(sys.argv) > 1:
		DB_PATH = sys.argv[1]
	try:
		print("Opening database at", DB_PATH)
		db = plyvel.DB(DB_PATH)
	except Exception as e:
		print(e)
		print("Database not found at", DB_PATH)
		return

	print("Database opened")
	sock = socket(AF_INET, SOCK_STREAM)
	sock.bind((IP, PORT))
	sock.listen()
	print("Server ready")
	try:
		while True:
			cliSock, _ = sock.accept()
			t = Thread(target=lambda: serveClient(cliSock))
			t.daemon = True
			t.start()
	except KeyboardInterrupt:
		print("Server stopped")
	finally:
		sock.close()
		db.close()

if __name__ == '__main__':
	main()
