// Package kms is the Eden Platform signing-key primitive — one canonical
// interface (KMSSigner) backed by three real HSM-class providers:
//
//   - awskms  — AWS GovCloud KMS (FIPS endpoints honored via AWS_USE_FIPS_ENDPOINT)
//   - azkeys  — Azure Managed HSM
//   - pkcs11  — PKCS#11 (SoftHSMv2 in dev; vendor HSM in prod IL5)
//
// There is NO in-memory or PEM-file provider. SoftHSMv2 is the dev parity path
// for PKCS#11; it is still exercised via the production code path. The AOID
// consumer (cmd/aoid/boot.go) rejects pkcs11:// URIs referencing softhsm in any
// non-dev environment.
//
// Provider URI schemes (parsed by net/url.Parse):
//
//   awskms:///arn:aws-us-gov:kms:us-gov-west-1:123:key/abcd
//       opaque path is the AWS KMS key ARN
//
//   azkeys://hsm-name.managedhsm.usgovcloudapi.net/keys/<key-name>/<key-version>
//       host is the Managed HSM FQDN; path carries key name + version
//
//   pkcs11:///etc/aoid/pkcs11.conf?label=<cka-label>
//       opaque path is the PKCS#11 module config; ?label= names the key
//
// KMSSigner embeds crypto.Signer so the returned values plug into golang-jwt,
// crypto/tls, and x509.CreateCertificate without conversion. HealthCheck
// performs a real Sign+Verify round-trip on a fixed canary payload so boot
// fails loudly when IAM allows GetPublicKey but denies Sign.
package kms
