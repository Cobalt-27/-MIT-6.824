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
	"sync"
	"sync/atomic"
	"time"

	//	"6.824/labgob"
	"6.824/labgob"
	"6.824/labrpc"
)

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
	follower        = 0
	candidate       = 1
	leader          = 2
	electionTimeout = 400
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
	lastHeartBeat int64
	role          int
	currentTerm   int
	votedFor      int
	lastVoteTerm  int

	log         []string
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
	log := make([]string, 0)
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
		if args.LastLogTerm > rf.lastLogTerm() || (args.LastLogTerm == rf.lastLogTerm() && args.LastlogIndex >= rf.lastLogIndex()) {
			reply.VoteGranted = true
			rf.votedFor = args.CandidateId
			rf.lastVoteTerm = args.Term
			rf.print("vote true to %d", args.CandidateId)
			return
		} else {
			reply.VoteGranted = false
		}
	}
	reply.VoteGranted = false
	rf.print("vote false to %d: votedFor=%d lastVoteTerm=%d args.Term=%d currentTerm=%d", args.CandidateId, rf.votedFor, rf.lastVoteTerm, args.Term, rf.currentTerm)
	// Your code here (2A, 2B).
}

type AppendEntriesArgs struct {
	Term         int
	LearderId    int
	PrevLogIndex int
	PrevLogTerm  int
	Entries      []interface{}
	LeaderCommit int
}

type AppendEntriesReply struct {
	Term    int
	Success bool
}

func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	replyFalse := func() {
		reply.Success = false
		reply.Term = rf.currentTerm
	}
	if len(args.Entries) == 0 { //heartbeat
		rf.print("recv heartbeat from %d", args.LearderId)
		if args.Term >= rf.currentTerm {
			rf.lastHeartBeat = time.Now().UnixMilli()
			rf.currentTerm = args.Term
			rf.role = follower
			reply.Term = rf.currentTerm
			reply.Success = true
		} else {
			rf.print("stale packet\n")
			replyFalse()
		}
	}

}

func (rf *Raft) print(format string, a ...interface{}) {
	return
	header := fmt.Sprintf("[%d,%d] ", rf.me, rf.currentTerm)
	fmt.Printf(header+format+"\n", a...)
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
	ok := rf.peers[server].Call("Raft.RequestVote", args, reply)
	return ok
}

func (rf *Raft) sendAppendEntries(server int, args *AppendEntriesArgs, reply *AppendEntriesReply) bool {
	ok := rf.peers[server].Call("Raft.AppendEntries", args, reply)
	return ok
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
	index := -1
	term := -1
	isLeader := true

	// Your code here (2B).

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
	rf.print("start ticker")
	time.Sleep(time.Duration(int64(rand.Intn(electionTimeout)+electionTimeout) * int64(time.Millisecond)))
	for !rf.killed() {
		rf.print("tick")
		rf.mu.Lock()
		if rf.role != leader && (time.Now().UnixMilli()-rf.lastHeartBeat) > electionTimeout { //timeout
			rf.role = candidate
			rf.mu.Unlock()
			rf.startElection()
			time.Sleep(time.Duration(int64(rand.Intn(electionTimeout)+electionTimeout) * int64(time.Millisecond)))

		} else {
			rf.mu.Unlock()
			time.Sleep(time.Duration(int64(rand.Intn(electionTimeout)+electionTimeout) * int64(time.Millisecond)))
		}

	}
}

func (rf *Raft) heartbeat() {
	rf.print("start heartbeat")
	for {
		rf.mu.Lock()
		if rf.role == leader {
			for i := 0; i < len(rf.peers); i++ {
				if i == rf.me {
					rf.lastHeartBeat = time.Now().UnixMilli()
					continue
				}
				args := AppendEntriesArgs{
					LearderId: rf.me,
					Term:      rf.currentTerm,
				}
				reply := AppendEntriesReply{
					Success: false,
				}
				go func(remote int, args AppendEntriesArgs, reply AppendEntriesReply) {
					rf.sendAppendEntries(remote, &args, &reply)
				}(i, args, reply)
			}
		}
		rf.mu.Unlock()
		time.Sleep(time.Duration(electionTimeout * int64(time.Millisecond) / 3))
	}
}

func (rf *Raft) lastLogIndex() int {
	return len(rf.log)
}
func (rf *Raft) lastLogTerm() int {
	idx := rf.lastLogIndex()
	if idx == 0 {
		return 0
	}
	return rf.logTerm[idx-1]
}

func (rf *Raft) startElection() { //whole process locked
	rf.mu.Lock()
	defer rf.mu.Unlock()
	rf.currentTerm++
	rf.votedFor = rf.me
	rf.lastVoteTerm = rf.currentTerm
	rf.print("starts election\n")
	voteChan := make(chan RequestVoteReply, len(rf.peers))
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
	for i := 0; i < len(rf.peers)-1; i++ {
		res := <-voteChan
		rf.print("get vote %t (%d)", res.VoteGranted, len(rf.peers))
		if res.VoteGranted {
			granted++
		} else {
			deny++
		}
		half := len(rf.peers) / 2
		if granted > half {
			//now leader
			rf.print("becomes leader")
			rf.role = leader
			return
		}
		if deny > half {
			rf.print("failed election (%d/%d)", deny, granted)
			rf.role = follower
			return
		}
	}
	rf.print("failed election")
	rf.role = follower
	//failed election
	// fmt.Printf("%d failed election\n", rf.me)
	// rf.role = follower
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
	// Your initialization code here (2A, 2B, 2C).
	rf.lastHeartBeat = time.Now().UnixMilli()

	// initialize from state persisted before a crash
	rf.readPersist(persister.ReadRaftState())

	// start ticker goroutine to start elections
	go rf.ticker()
	go rf.heartbeat()
	return rf
}
