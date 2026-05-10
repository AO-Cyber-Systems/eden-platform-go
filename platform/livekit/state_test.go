package livekit

import "testing"

func TestValidCallTransition(t *testing.T) {
	cases := []struct {
		from, to CallState
		want     bool
	}{
		// ringing →
		{StateRinging, StateConnecting, true},
		{StateRinging, StateDeclined, true},
		{StateRinging, StateMissed, true},
		{StateRinging, StateFailed, true},
		{StateRinging, StateConnected, false},
		{StateRinging, StateEnded, false},
		// connecting →
		{StateConnecting, StateConnected, true},
		{StateConnecting, StateFailed, true},
		{StateConnecting, StateEnded, true},
		{StateConnecting, StateRinging, false},
		// connected →
		{StateConnected, StateEnded, true},
		{StateConnected, StateConnecting, false},
		{StateConnected, StateRinging, false},
		// terminal states never transition
		{StateEnded, StateRinging, false},
		{StateMissed, StateConnecting, false},
		{StateDeclined, StateEnded, false},
		{StateFailed, StateConnecting, false},
	}
	for _, c := range cases {
		if got := ValidCallTransition(c.from, c.to); got != c.want {
			t.Errorf("ValidCallTransition(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}

func TestCallStateIsTerminal(t *testing.T) {
	terminal := []CallState{StateDeclined, StateMissed, StateFailed, StateEnded}
	for _, s := range terminal {
		if !s.IsTerminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
	nonTerminal := []CallState{StateRinging, StateConnecting, StateConnected}
	for _, s := range nonTerminal {
		if s.IsTerminal() {
			t.Errorf("%s should NOT be terminal", s)
		}
	}
}

func TestValidMeetingTransition(t *testing.T) {
	cases := []struct {
		from, to MeetingState
		want     bool
	}{
		{MeetingStateScheduled, MeetingStateActive, true},
		{MeetingStateScheduled, MeetingStateEnded, true},
		{MeetingStateActive, MeetingStateEnded, true},
		{MeetingStateActive, MeetingStateScheduled, false},
		{MeetingStateEnded, MeetingStateActive, false},
	}
	for _, c := range cases {
		if got := ValidMeetingTransition(c.from, c.to); got != c.want {
			t.Errorf("ValidMeetingTransition(%s, %s) = %v, want %v", c.from, c.to, got, c.want)
		}
	}
}
