// Package livekit provides Eden's platform-level abstraction over the LiveKit
// real-time video/audio stack.
//
// The package supports four capabilities, each independently consumable:
//
//  1. 1:1 calling — InitiateCall / AcceptCall / DeclineCall / EndCall, driven
//     by a state machine (ringing → connecting → connected → ended).
//  2. Multi-party meetings — CreateMeeting / JoinMeeting / LeaveMeeting /
//     EndMeeting, with bounded participant counts and lifecycle hooks.
//  3. Egress recording — RecordingService.StartRecording / StopRecording, with
//     metadata persisted via the consumer-supplied Store.
//  4. Webhook receiver — WebhookHandler verifies LiveKit's HMAC signatures
//     and dispatches participant_joined / participant_left / room_finished /
//     egress_ended events to internal handlers and consumer-registered
//     subscribers.
//
// Consumers wire up an implementation by providing four interfaces:
//
//   - Store     — persistence for calls, meetings, recordings.
//     The platform ships an InMemoryStore for tests; production callers
//     write a postgres-backed implementation.
//   - Rooms     — LiveKit room CRUD. The shipped LiveKitAdapter implements it.
//   - Tokens    — JWT issuance for LiveKit room joins. Same adapter implements.
//   - Signaler  — push events to specific users (NATS, WebSocket, etc.).
//
// This package is the platform extraction of eden-circle's calling subsystem
// (per portfolio decision D13). Eden Family is its first net-new consumer.
package livekit
