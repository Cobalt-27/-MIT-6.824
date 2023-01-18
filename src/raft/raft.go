package raft

//
// this is an outline of the API that raft must expose to
// the service (or tester). see comments below for
// each of these functions for more details.
//
// rf = Make(...)
//   create a new Raft server.
// rf.Start(command interface{}) (index, term, isleader)
//   start agreement on a new log entry
// rf.GetState() (term, isLeader)
//   ask a Raft for its current term, and whether it thinks it is leader
// ApplyMsg
//   each time a new entry is committed to the log, each Raft peer
//   should send an ApplyMsg to the service (or tester)
//   in the same server.
//

import (
	//	"bytes"
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"sort"
	"sync"
	"sync/atomic"
	"time"
	//	"6.824/labgob"
	"6.824/labgob"
	"6.824/labrpc"
)

const electionTimeout int64 = 400
const printDebug = true

//
// as each Raft peer becomes aware that successive log entries are
// committed, the peer should send an ApplyMsg to the service (or
// tester) on the same server, via the applyCh passed to Make(). set
// CommandValid to true to indicate that the ApplyMsg contains a newly
// committed log entry.
//
// in part 2D you'll want to send other kinds of messages (e.g.,
// snapshots) on the applyCh, but set CommandValid to false for these
// other uses.
//
type ApplyMsg struct {
	CommandValid bool
	Command      interface{}
	CommandIndex int

	// For 2D:
	SnapshotValid bool
	Snapshot      []byte
	SnapshotTerm  int
	SnapshotIndex int
}

//
// A Go object implementing a single Raft peer.
//
const (
	follower  = 0
	candidate = 1
	leader    = 2
)

type Raft struct {
	mu        sync.Mutex          // Lock to protect shared access to this peer's state
	peers     []*labrpc.ClientEnd // RPC end points of all peers
	persister *Persister          // Object to hold this peer's persisted state
	me        int                 // this peer's index into peers[]
	dead      int32               // set by Kill()
	// Your data here (2A, 2B, 2C).
	// Look at the paper's Figure 2 for a description of what
	// state a Raft server must maintain.
	applyChan     chan ApplyMsg
	lastHeartBeat int64
	role          int
	currentTerm   int
	votedFor      int
	lastVoteTerm  int

	log         []interface{}
	logTerm     []int
	commitIndex int
	lastApplied int

	nextIndex  []int
	matchIndex []int
}

// return currentTerm and whether this server
// believes it is the leader.
func (rf *Raft) GetState() (int, bool) {
	// Your code here (2A).
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm, rf.role == leader
}

func (rf *Raft) majority() int {
	return rf.peerCount()/2 + 1
}

func (rf *Raft) failCount() int {
	return rf.peerCount() - rf.majority() + 1
}

func (rf *Raft) peerCount() int {
	return len(rf.peers)
}

func (rf *Raft) size() int {
	return len(rf.log)
}

//
// save Raft's persistent state to stable storage,
// where it can later be retrieved after a crash and restart.
// see paper's Figure 2 for a description of what should be persistent.
//
func (rf *Raft) persist() {
	w := new(bytes.Buffer)
	e := labgob.NewEncoder(w)
	e.Encode(rf.currentTerm)
	e.Encode(rf.votedFor)
	e.Encode(rf.log)
	data := w.Bytes()
	rf.persister.SaveRaftState((data))
}

//
// restore previously persisted state.
//
func (rf *Raft) readPersist(data []byte) {
	if data == nil || len(data) < 1 { // bootstrap without any state?
		return
	}
	// Your code here (2C).
	// Example:
	r := bytes.NewBuffer(data)
	d := labgob.NewDecoder(r)
	currentTerm := 0
	votedFor := 0
	log := make([]interface{}, 0)
	if d.Decode(&currentTerm) != nil ||
		d.Decode(&votedFor) != nil ||
		d.Decode(&log) != nil {
		panic(1)
	} else {
		rf.currentTerm = currentTerm
		rf.votedFor = votedFor
		rf.log = log
	}
}

//
// A service wants to switch to snapshot.  Only do so if Raft hasn't
// have more recent info since it communicate the snapshot on applyCh.
//
func (rf *Raft) CondInstallSnapshot(lastIncludedTerm int, lastIncludedIndex int, snapshot []byte) bool {

	// Your code here (2D).

	return true
}

// the service says it has created a snapshot that has
// all info up to and including index. this means the
// service no longer needs the log through (and including)
// that index. Raft should now trim its log as much as possible.
func (rf *Raft) Snapshot(index int, snapshot []byte) {
	// Your code here (2D).

}

//
// example RequestVote RPC arguments structure.
// field names must start with capital letters!
//
type RequestVoteArgs struct {
	Term         int
	CandidateId  int
	LastlogIndex int
	LastLogTerm  int
	// Your data here (2A, 2B).
}

//
// example RequestVote RPC reply structure.
// field names must start with capital letters!
//
type RequestVoteReply struct {
	Term        int
	VoteGranted bool
	// Your data here (2A).
}

//
// example RequestVote RPC handler.
//
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.currentTerm
	if (rf.lastVoteTerm < args.Term || rf.votedFor == args.CandidateId) && rf.currentTerm <= args.Term {
		if rf.isUpToDate(args.LastlogIndex, args.LastLogTerm) {
			reply.VoteGranted = true
			rf.votedFor = args.CandidateId
			rf.lastVoteTerm = args.Term
			rf.debug("vote true to %d", args.CandidateId)
			return
		} else {
			rf.debug("vote false to %d", args.CandidateId)
			reply.VoteGranted = false
			return
		}
	}
	reply.VoteGranted = false
	rf.debug("vote false to %d: votedFor=%d lastVoteTerm=%d args.Term=%d currentTerm=%d", args.CandidateId, rf.votedFor, rf.lastVoteTerm, args.Term, rf.currentTerm)
	// Your code here (2A, 2B).
}

type LogEntry struct {
	EntryTerm int
	EntryVal  interface{}
}

type AppendEntriesArgs struct {
	Term         int
	LearderId    int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []LogEntry
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int
	Success bool
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.currentTerm

	if args.Term < rf.currentTerm {
		reply.Success = false
		rf.debug("stale packet term=%d", args.Term)
		return
	}
	prevCorrect := func(prevIndex int, prevTerm int) bool {
		if args.PrevLogIndex > rf.lastLogIndex() { //index do not exist
			return false
		}
		var myPrevTerm int
		if args.PrevLogIndex < 0 { //special case
			myPrevTerm = 0
		} else {
			myPrevTerm = rf.logTerm[args.PrevLogIndex]
		}
		if myPrevTerm != args.PrevLogTerm {
			return false
		} else {
			return true
		}
	}
	//up to date
	if len(args.Entries) == 0 { //heartbeat
		// rf.debug("recv heartbeat from %d", args.LearderId)
		rf.lastHeartBeat = time.Now().UnixMilli()
		rf.currentTerm = args.Term
		rf.role = follower
		reply.Term = rf.currentTerm
		if prevCorrect(args.PrevLogIndex, args.PrevLogTerm) {
			rf.commitIndex = min(args.LeaderCommit, rf.lastLogIndex())
			reply.Success = true
			rf.debug("COMMIT<-%d empty log", rf.commitIndex)
			return
		} else {
			reply.Success = false
			return
		}
	} else {
		//check prev
		if args.PrevLogIndex > rf.lastLogIndex() {
			reply.Success = false //index do not exist
			return
		}
		var myPrevTerm int
		if args.PrevLogIndex < 0 { //special case
			myPrevTerm = 0
		} else {
			myPrevTerm = rf.logTerm[args.PrevLogIndex]
		}
		if myPrevTerm != args.PrevLogTerm {
			reply.Success = false
			return
		} else {
			addCount := len(args.Entries)
			newLog := make([]interface{}, addCount)
			newLogTerm := make([]int, addCount)
			for i := range args.Entries {
				newLog[i] = args.Entries[i].EntryVal
				newLogTerm[i] = args.Entries[i].EntryTerm
			}
			rf.log = append(rf.log[:args.PrevLogIndex+1], newLog...)
			rf.logTerm = append(rf.logTerm, newLogTerm...)
			rf.commitIndex = min(args.LeaderCommit, rf.lastLogIndex())
			rf.debug("added %d log", addCount)
			rf.debug("COMMIT<-%d", rf.commitIndex)
			reply.Success = true
			return
		}
	}

}

func min(a int, b int) int {
	if a < b {
		return a
	} else {
		return b
	}

}

func (rf *Raft) debug(format string, a ...interface{}) { //may produce race
	if printDebug {
		header := fmt.Sprintf("[%d,%d] ", rf.me, rf.currentTerm)
		fmt.Printf(header+format+"\n", a...)
	}
}

func (rf *Raft) isUpToDate(lastLogIndex int, lastLogTerm int) bool {
	return lastLogTerm > rf.lastLogTerm() || (lastLogTerm == rf.lastLogTerm() && lastLogIndex >= rf.lastLogIndex())
}

func (rf *Raft) sleep(milliSecond int64) {
	time.Sleep(time.Duration(milliSecond * int64(time.Millisecond)))
}

//
// example code to send a RequestVote RPC to a server.
// server is the index of the target server in rf.peers[].
// expects RPC arguments in args.
// fills in *reply with RPC reply, so caller should
// pass &reply.
// the types of the args and reply passed to Call() must be
// the same as the types of the arguments declared in the
// handler function (including whether they are pointers).
//
// The labrpc package simulates a lossy network, in which servers
// may be unreachable, and in which requests and replies may be lost.
// Call() sends a request and waits for a reply. If a reply arrives
// within a timeout interval, Call() returns true; otherwise
// Call() returns false. Thus Call() may not return for a while.
// A false return can be caused by a dead server, a live server that
// can't be reached, a lost request, or a lost reply.
//
// Call() is guaranteed to return (perhaps after a delay) *except* if the
// handler function on the server side does not return.  Thus there
// is no need to implement your own timeouts around Call().
//
// look at the comments in ../labrpc/labrpc.go for more details.
//
// if you're having trouble getting RPC to work, check that you've
// capitalized all field names in structs passed over RPC, and
// that the caller passes the address of the reply struct with &, not
// the struct itself.
//
func (rf *Raft) sendRequestVote(server int, args *RequestVoteArgs, reply *RequestVoteReply) bool {
	rf.debug("RequestVote %d", server)
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	if len(args.Entries) != 0 {
		rf.debug("AppendEntries -> %d len=%d", server, len(args.Entries))
	}
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
}

func (rf *Raft) makeEntries(start int, end int) []LogEntry {
	len := end - start
	entries := make([]LogEntry, len)
	for i := start; i < end; i++ {
		entries[i] = LogEntry{
			EntryTerm: rf.logTerm[i],
			EntryVal:  rf.log[i],
		}
	}
	return entries
}

func (rf *Raft) syncLog() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.role != leader {
		return
	}
	for remote := range rf.peers {
		if remote == rf.me {
			continue
		}
		if rf.nextIndex[remote] <= rf.lastLogIndex() {
			rf.debug("sync range=%d-%d", rf.nextIndex[remote], rf.lastLogIndex())
			replyChan := make(chan AppendEntriesReply)
			prev := rf.nextIndex[remote] - 1
			var prevTerm int
			if prev < 0 {
				prevTerm = 0
			} else {
				prevTerm = rf.logTerm[prev]
			}
			args := AppendEntriesArgs{
				Term:         rf.currentTerm,
				LearderId:    rf.me,
				PrevLogIndex: prev,
				PrevLogTerm:  prevTerm,
				Entries:      rf.makeEntries(rf.nextIndex[remote], rf.size()),
				LeaderCommit: rf.commitIndex,
			}
			go func(to int, args AppendEntriesArgs) {
				reply := AppendEntriesReply{}
				rf.sendAppendEntries(to, &args, &reply)
				replyChan <- reply
			}(remote, args)
			go func(from int, next int) {
				reply := <-replyChan
				rf.mu.Lock()
				defer rf.mu.Unlock()
				rf.debug("recv AppendEntries reply %v", reply.Success)
				if reply.Success {
					rf.nextIndex[from] = next
					rf.matchIndex[from] = next - 1
				} else {
					if rf.nextIndex[from] > 0 {
						rf.nextIndex[from]--
					}
				}
			}(remote, rf.lastLogIndex()+1)
		}
	}
}

func (rf *Raft) updateCommit() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.role != leader || rf.size() == 0 {
		return
	}
	match := make([]int, rf.peerCount())
	copy(match, rf.matchIndex)
	sort.Ints(match)
	newCommit := match[rf.majority()-1]
	if rf.logTerm[newCommit] == rf.currentTerm && rf.commitIndex < newCommit {
		rf.commitIndex = newCommit
		rf.debug("COMMIT<-%d", newCommit)
	}
}

func (rf *Raft) checkApply() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.lastApplied >= 0 && rf.lastApplied < rf.commitIndex {
		for i := rf.lastApplied; i <= rf.commitIndex; i++ {
			rf.apply(i)
		}
	}
}

//thread unsafe
func (rf *Raft) apply(index int) {
	rf.debug("APPLY %d", index)
}

//
// the service using Raft (e.g. a k/v server) wants to start
// agreement on the next command to be appended to Raft's log. if this
// server isn't the leader, returns false. otherwise start the
// agreement and return immediately. there is no guarantee that this
// command will ever be committed to the Raft log, since the leader
// may fail or lose an election. even if the Raft instance has been killed,
// this function should return gracefully.
//
// the first return value is the index that the command will appear at
// if it's ever committed. the second return value is the current
// term. the third return value is true if this server believes it is
// the leader.
//
func (rf *Raft) Start(command interface{}) (int, int, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.debug("START new agreement")
	index := -1
	term := -1
	isLeader := rf.role == leader
	if isLeader {
		index = rf.lastLogIndex() + 1
		term = rf.currentTerm
		rf.log = append(rf.log, command)
		rf.logTerm = append(rf.logTerm, term)
	}

	// Your code here (2B).
	rf.debug("START return: index=%v term=%v isLeader=%v", index, term, isLeader)
	return index, term, isLeader
}

//
// the tester doesn't halt goroutines created by Raft after each test,
// but it does call the Kill() method. your code can use killed() to
// check whether Kill() has been called. the use of atomic avoids the
// need for a lock.
//
// the issue is that long-running goroutines use memory and may chew
// up CPU time, perhaps causing later tests to fail and generating
// confusing debug output. any goroutine with a long-running loop
// should call killed() to check whether it should stop.
//
func (rf *Raft) Kill() {
	atomic.StoreInt32(&rf.dead, 1)
	// Your code here, if desired.
}

func (rf *Raft) killed() bool {
	z := atomic.LoadInt32(&rf.dead)
	return z == 1
}

// The ticker go routine starts a new election if this peer hasn't received
// heartsbeats recently.
func (rf *Raft) ticker() {
	tickerSleep := func() {
		rf.sleep(rand.Int63n(electionTimeout) + electionTimeout)
	}
	tickerSleep()
	for !rf.killed() {
		// rf.debug("tick")
		rf.mu.Lock()
		if rf.role != leader && (time.Now().UnixMilli()-rf.lastHeartBeat) > electionTimeout { //timeout
			rf.role = candidate
			term := rf.currentTerm
			rf.mu.Unlock()
			rf.startElection(term)
		} else {
			rf.mu.Unlock()
		}
		tickerSleep()
	}
}

func (rf *Raft) heartbeat() {
	for {
		rf.mu.Lock()
		rf.sendHeartbeats()
		rf.mu.Unlock()
		rf.sleep(electionTimeout / 3)
	}
}

func (rf *Raft) sendHeartbeats() {
	if rf.role == leader {
		args := AppendEntriesArgs{
			LearderId: rf.me,
			Term:      rf.currentTerm,
		}
		reply := AppendEntriesReply{}
		for remote := range rf.peers {
			if remote == rf.me {
				rf.lastHeartBeat = time.Now().UnixMilli()
				continue
			}
			go func(to int, args AppendEntriesArgs, reply AppendEntriesReply) {
				rf.sendAppendEntries(to, &args, &reply)
			}(remote, args, reply)
		}
	}
}

//External LastLogIndex, -1 for empty log. Thread unsafe.
func (rf *Raft) lastLogIndex() int {
	return rf.size() - 1
}

//External LastLogTerm, 0 for empty log. Thread unsafe.
func (rf *Raft) lastLogTerm() int {
	idx := rf.lastLogIndex()
	if idx < 0 {
		return 0
	}
	return rf.logTerm[idx]
}

//Start Election.\
func (rf *Raft) startElection(oldTerm int) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if oldTerm != rf.currentTerm || rf.role != candidate {
		return
	}
	rf.currentTerm++

	startTerm := rf.currentTerm
	rf.votedFor = rf.me
	rf.lastVoteTerm = rf.currentTerm
	rf.role = candidate

	rf.debug("starts election\n")

	voteChan := make(chan RequestVoteReply, rf.peerCount())
	args := RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateId:  rf.me,
		LastlogIndex: rf.lastLogIndex(),
		LastLogTerm:  rf.lastLogTerm(),
	}
	reply := RequestVoteReply{
		VoteGranted: false,
	}

	for remote := range rf.peers {
		if remote == rf.me {
			continue
		}
		go func(peer int, args RequestVoteArgs, reply RequestVoteReply) {
			rf.sendRequestVote(peer, &args, &reply)
			voteChan <- reply
		}(remote, args, reply)
	}

	granted := 1 //1 vote from self
	deny := 0
	winCount := rf.majority()
	loseCount := rf.peerCount() - winCount + 1
	for i := 0; i < rf.peerCount()-1; i++ {
		rf.debug("waiting for vote")
		rf.mu.Unlock()
		res := <-voteChan
		rf.mu.Lock()
		rf.debug("get vote %t (%d)", res.VoteGranted, rf.peerCount())
		if rf.role != candidate || rf.currentTerm != startTerm {
			return
		}
		if res.VoteGranted {
			granted++
		} else {
			deny++
		}
		if granted >= winCount {
			//now leader
			rf.debug("becomes leader")
			rf.role = leader
			rf.nextIndex = make([]int, rf.peerCount())
			for remote := range rf.peers {
				rf.nextIndex[remote] = rf.lastLogIndex() + 1
			}
			rf.matchIndex = make([]int, rf.peerCount())
			rf.sendHeartbeats()
			return
		}
		if deny >= loseCount {
			rf.debug("failed election (%d/%d)", deny, granted)
			rf.role = follower
			return
		}
	}
	os.Exit(-1)
}

//
// the service or tester wants to create a Raft server. the ports
// of all the Raft servers (including this one) are in peers[]. this
// server's port is peers[me]. all the servers' peers[] arrays
// have the same order. persister is a place for this server to
// save its persistent state, and also initially holds the most
// recent saved state, if any. applyCh is a channel on which the
// tester or service expects Raft to send ApplyMsg messages.
// Make() must return quickly, so it should start goroutines
// for any long-running work.
//
func Make(peers []*labrpc.ClientEnd, me int,
	persister *Persister, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{}
	rf.peers = peers
	rf.persister = persister
	rf.me = me
	rf.currentTerm = 0
	rf.applyChan = applyCh
	rf.lastApplied = -1
	rf.nextIndex = make([]int, len(peers))
	rf.role = follower

	// Your initialization code here (2A, 2B, 2C).
	rf.lastHeartBeat = time.Now().UnixMilli()

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()
	go rf.heartbeat()
	go func() {
		for {
			rf.syncLog()
			rf.updateCommit()
			rf.checkApply()
			rf.sleep(electionTimeout / 3)
		}
	}()
	return rf
}
