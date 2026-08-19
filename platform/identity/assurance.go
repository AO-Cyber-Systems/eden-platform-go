package identity

// DeriveAssurance reports the assurance level an authentication actually
// reached, as one of the AAL1, AAL2 and AAL3 rungs the claim contract defines.
//
// # The ladder
//
// The derivation counts distinct factor CATEGORIES, never factors:
//
//   - No factors is an error rather than a level. Nothing was established, so
//     no rung describes it. Returning the lowest one here would read as
//     "authenticated weakly" when the truth is "not authenticated" — the exact
//     overstatement this function exists to prevent.
//
//   - One distinct category is AAL1, however many methods were exercised
//     within it. A password and a security question are two prompts and one
//     category: the second adds friction rather than assurance, because
//     whatever compromises the first — a phished secret, a breached reuse, a
//     shoulder-surfed screen — tends to compromise the second as well.
//
//   - Two or more distinct categories is AAL2. This is the multi-factor rung,
//     and what earns it is independence: a knowledge factor and a possession
//     factor fail to different attacks.
//
//   - Two or more categories PLUS both asserted properties is AAL3. The top
//     rung additionally requires a hardware-resident authenticator and
//     resistance to verifier impersonation. Neither property is visible in a
//     factor's name — a method called "webauthn" may or may not be hardware
//     backed, depending on how it was deployed — so both are read from what
//     the verifier asserted, never inferred here.
//
// Both properties asserted WITHOUT two categories does not reach AAL3. They
// qualify a multi-factor authentication; they do not substitute for one.
//
// # It never rounds upward
//
// Every gap and every ambiguity resolves to the lower rung. An unrecognised
// factor category is rejected rather than counted, and a malformed
// authentication yields an error rather than a guess. The asymmetry is the
// point: understating assurance costs a principal an extra prompt, while
// overstating it silently grants access a policy meant to withhold and leaves
// an audit record asserting a control that was never applied.
//
// A consumer whose policy maps these facts differently is free to adjust the
// level before minting. What must not happen is this function quietly doing it
// on their behalf.
//
// # It is pure
//
// The same authentication always yields the same level. There is no clock, no
// configuration and no package state involved, and the authentication is not
// modified. An error is returned with no level at all, so a caller that
// mishandles it cannot pick up a rung that was never reached.
func DeriveAssurance(auth *Authentication) (string, error) {
	if err := auth.Validate(); err != nil {
		return "", err
	}

	// Kinds has already collapsed duplicate categories and dropped anything
	// unrecognised, so this is a count of independent lines of defence rather
	// than a count of prompts.
	categories := len(auth.Kinds())

	// Fewer than two independent categories cannot be multi-factor. The
	// comparison is deliberately "< 2" rather than "== 1": if a future change
	// ever let an authentication through with no countable category, it lands
	// on the bottom rung instead of falling through to a higher one.
	if categories < 2 {
		return AAL1, nil
	}

	// Both properties are required, and both are read from what the verifier
	// asserted. An unset property means "not vouched for", which is the safe
	// reading — it is never treated as unknown-and-therefore-probably-true.
	if auth.HardwareBacked && auth.PhishingResistant {
		return AAL3, nil
	}

	return AAL2, nil
}
