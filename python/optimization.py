from math import floor, ceil
import sys
import os
from datetime import timedelta
import click

#
# Constants
#

eps = 1/2048
c  = 1+eps				# Base of the exponential for difficulty increase
ctilde = 1-99*eps		# Base of the exponential for difficulty decrease
B  = 192				# Size of a batch
Tb = 13					# Time for the network to mine a new block
K  = 64					# Snap sync verifies one every K blocks
f_threshold = Tb/9/K	# For f <= f_threshold, constant-difficulty strategy achieves theoretical TD

#
# Parameters
#

f     =  0.45    		# Fraction of the network mining power controlled by the adversary
Tm    =  45*60			# Available time to produce the fake chain segment
w     =  9				# deltaTimestamp for last 88 blocks
eta   =  None			# Relative target difficulty to reach at the end of the fake chain segment,
						# i.e. eta := target_difficulty/initial_difficulty
delta =  0.05			# Allowed deviation of the final difficulty from the target difficulty

# Setting a target difficulty may be useful for two reasons:
# 1. The adversary may want the final difficulty to be the same as in the beginning,
#    in order to keep the victim's suspicion at a minimum
#    (targetGoal = 'original')
# 2. The adversary may want the final difficulty to be low enough, so that he can
#    still mine new blocks every Tb seconds with his fraction of mining power,
#    in order to convince the victim that the chain isn't stale
#    (targetGoal = 'liveness')





# Returns the additional difficulty the network generates during the time of the attack.
def networkProgress():
	return floor(Tm/Tb)


# Returns the total difficulty of the adversary's chain segment, if blocks were mined
# at constant difficulty d0 and if future blocks weren't an issue.
def theoreticalTotalDifficulty():
	global Tb
	global K
	global f
	global Tm

	timeFor88 = 88*Tb/f
	timeMining = Tm - timeFor88

	td = timeMining/Tb * K * f + 88
	return td


# Returns the total difficulty of the adversary's chain segment if
# constant-difficulty strategy is used.
def tdWithConstantDiffStrategy():
	global Tm
	return min(Tm/9, theoreticalTotalDifficulty())


# Returns the total difficulty of the adversary's chain segment if
# modulate-difficulty strategy is used, given n, m, q
def totalDifficulty(n, m, q):
	global c
	global B

	term1 = q * (1-c**floor((B*n-m)/q))/(1-c) + c**floor((B*n-m)/q) - 1
	term2 = (q - (m%q if m%q!=0 else q)) * c**floor((B*n-m)/q)
	term3 = c**floor((B*n-m)/q) * ctilde * (1-ctilde**m)/(1-ctilde)
	term4 = 88*c**floor((B*n-m)/q)*ctilde**m

	return term1 + term2 + term3 + term4


# Computes the required effort to achieve this choice of n, m, q
def effort(n, m, q):
	global c
	global ctilde
	global B
	global f
	global K
	global Tm
	global Tb

	# In this case the computation is different because q does not divide K
	if q==B:
		base = c**n if m==0 else c**(n-1)
		term1 = B/K * (1-c**(n-1))/(1-c) + (B/K-ceil(m/K))*c**(n-1) - 1 + base
		term2 = c**(n-1) * ctilde**(m%K if m%K!=0 else K) * (1-ctilde**(K*ceil(m/K)))/(1-ctilde**K)
		term3 = 88*base*ctilde**m
	else:
		term1 = c**(K/q) * (1-c**(B*n/q-ceil(m/K)*K/q))/(1-c**(K/q))
		term2 = c**(B*n/q-ceil(m/q)) * ctilde**(m%K if m%K!=0 else K) * (1-ctilde**(ceil(m/K)*K))/(1-ctilde**K)
		term3 = 88*c**(B*n/q - ceil(m/q))*ctilde**m

	return term1 + term2 + term3


# Returns a boolean indicating whether this choice of n, m, q is achievable
# according to the effort limit.
def enoughEffort(n, m, q):
	global Tb
	global Tm
	global f

	eff = effort(n, m, q)
	available = Tm/Tb * f

	if available >= eff:
		return True
	return False


# Returns the deltaTimestamp for difficulty-increasing blocks, noted as 'z' in the Notes
# If the returned value is >= 9, it means that there is enough time and there will be no future blocks,
# regardless of the value of z.
def deltaTimestamp(n, m, q):
	global B
	global Tm
	global w

	factor1 = floor((B*n-m)/q)
	factor2 = q - (m%q if m%q!=0 else q)

	return (Tm - 9*factor2 - 900*m - 88*w)/factor1 - (q-1)*9


# Returns the target difficulty, given n, m, q
def computeFinalDifficulty(n, m, q):
	global c
	global ctilde
	global B

	return c**floor((B*n-m)/q)*ctilde**m


class TargetChecker:
	def check(n, m, q):
		pass

class LivenessTargetChecker(TargetChecker):
	def check(n, m, q):
		global f
		global delta
		return computeFinalDifficulty(n, m, q) <= (1+delta)*f

class ValueTargetChecker(TargetChecker):
	def check(n, m, q):
		global f
		global eta
		global delta
		return (1-delta)*eta <= computeFinalDifficulty(n, m, q) <= (1+delta)*eta


@click.command()
@click.option('--fraction')
@click.option('--time')
@click.option('--tgoal')
@click.option('--export')
def main(fraction, time, tgoal, export):
	global K
	global Tb
	global eta
	global f
	global Tm

	targetChecker = None
	exportEnabled = False

	for arg in sys.argv:
		if 'fraction' in arg:
			f = float(fraction)
		if 'time' in arg:
			Tm = int(time)
		if 'export' in arg:
			exportEnabled = True

		# Find out if we have a target difficulty
		'''
		if len(sys.argv) > 1 and 'tgoal' in sys.argv[1]:
			targetGoal = sys.argv[1].split('=')[-1].split()[-1]
		'''
		if 'tgoal' in arg:
			targetGoal = tgoal
			if targetGoal == 'liveness':
				targetChecker = LivenessTargetChecker
			elif targetGoal == 'original':
				eta = 1
				targetChecker = ValueTargetChecker
			else:
				try:
					eta = float(targetGoal)
					targetChecker = ValueTargetChecker
				except ValueError:
					pass



	# All potential values for q are the divisors of K...
	valuesForQ = list()
	for i in range(1, K+1):
		if K % i == 0:
			valuesForQ.append(i)
	valuesForQ.append(B) # ...and also B


	print(f'Computing for f = {f}, Tm = {Tm}\n')

	maxx  =  0
	n_max = -1
	m_max = -1
	q_max = -1

	for q in valuesForQ:
		n = 1
		localMaxx = 0
		local_n_max = -1
		local_m_max = -1

		# If there is not enough effort to create n batches where the last one has ALL
		# blocks as difficulty-decreasing blocks, then there is not enough effort to create
		# n batches where the last one has only SOME blocks as difficulty-decreasing blocks.
		# In addition, there is not enough effort for n' > n batches either. So we can go to
		# the next value for q
		while enoughEffort(n, B, q):
			for m in range(B, -1, -1):

				# If the adversary can't afford this effort, try other params
				if not enoughEffort(n, m, q):
					continue

				# This condition can hold only if n==1 and m>=1 and it means that we never increase the difficulty,
				# while we decrease it on at least one block.
				# In a batch, the network makes progress of 192*d0, while the adversary surely makes less progress
				# as he still produces 192 blocks, but with lower difficulty on at least the last block.
				# Therefore these params can never lead to a positive advantage and can be skipped.
				if B*n-m < q:
					continue

				# At this point, we are really using the modulate-difficulty strategy. If these params do not
				# allow to stay within the time limit, try other ones
				if deltaTimestamp(n, m, q) < 1:
					continue

				if targetChecker and targetChecker.check(n, m, q) is False:
					continue

				td = totalDifficulty(n, m, q)
				if td > maxx:
					maxx = td
					n_max = n
					m_max = m
					q_max = q
				if td > localMaxx:
					localMaxx = td
					local_n_max = n
					local_m_max = m
			n += 1
		print('TD =', floor(localMaxx), '\t@ n = %3d,\tm = %3d,\tq = %3d,\tdeltaTimestamp = %.2f' %
			 (local_n_max, local_m_max, q, deltaTimestamp(local_n_max, local_m_max, q)))


	modulDiffTd = floor(maxx)
	constDiffTd = floor(tdWithConstantDiffStrategy())
	theoreticalTd = floor(theoreticalTotalDifficulty())
	bestTd = max(modulDiffTd, constDiffTd)
	bestStrategy = None
	print('\nOptimal total difficulty with modulate-difficulty strategy:',
		   modulDiffTd, '\b @ n =', n_max, '\b, m =', m_max, '\b, q =', q_max,
		  '\b, deltaTimestamp = %.2f' % deltaTimestamp(n_max, m_max, q_max))
	print('Total difficulty with constant-difficulty strategy:', constDiffTd)
	print('Theoretical total difficulty (with no strategy):', theoreticalTd)
	print()

	os.system("")
	if modulDiffTd > constDiffTd:
		print("\033[0;32mModulate-difficulty strategy is better\033[0m")
		bestStrategy = "modulate"
	else:
		print('\033[0;33mConstant-difficulty strategy is better\033[0m')
		bestStrategy = "constant"
		if targetChecker:
			print('\033[0;33mWARNING: target difficulty is set, but constant-difficulty strategy ignores it\033[0m')
	if modulDiffTd > theoreticalTd:
		print('\033[0;32mModulate-difficulty strategy improves on theoretical value!\033[0m')
	print()

	advantage = bestTd - networkProgress()
	if advantage > 0:
		print('Difficulty advantage (using best strategy):', advantage)
		print('Time advantage (using best strategy):', timedelta(seconds=(advantage*Tb)), 'hours')
	else:
		print('\033[0;31mAttack is infeasible with chosen parameters\033[0m')


	if exportEnabled:
		if advantage > 0:
			with open(export, 'w') as f:
				f.write(bestStrategy+'\n')
				if bestStrategy == "modulate":
					f.write(f'{n_max}\n{m_max}\n{q_max}\n{deltaTimestamp(n_max, m_max, q_max)}')
		else:
			with open(export, 'w') as f:
				f.write("none" + '\n')





if __name__ == '__main__':
	main()