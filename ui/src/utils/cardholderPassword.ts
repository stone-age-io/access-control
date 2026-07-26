/**
 * Every cardholder create must carry a password, even for the vast majority of people
 * who will never sign in.
 *
 * # Why the client has to do this
 *
 * `cardholders` is a PocketBase AUTH collection (one person is one record, whether or
 * not they have a badge login). PocketBase's record-create REQUEST form validates
 * `password` and `passwordConfirm` as required on any new auth record —
 * `forms.RecordUpsert.validate`, where `isNew` forces both — and that runs before any
 * `OnRecordCreate` hook, so accessd cannot fill it in for an HTTP caller. It does fill
 * it for programmatic writes (`badgeapi.bindPasswordFill`, which covers the visitor
 * mint, the dev fixture, and any Go-side import), but the collection API is out of its
 * reach.
 *
 * PocketBase has the same problem in its own OAuth2 sign-up path and solves it the same
 * way — `apis.recordCreate` injects `security.RandomString(30)` when the request carries
 * no password. This is that, on our side of the wire.
 *
 * # Why a random value and not a constant
 *
 * A shared default across an install's records would be a de-facto shared secret the
 * moment `badge_login` was ticked for anyone. This value is never shown, never stored
 * client-side, and never sent anywhere but the create request; a person who is later
 * given a badge login gets a real password from the operator, or signs in with an
 * emailed code and sets their own.
 *
 * It is not a way in on its own regardless: the collection's auth rule requires
 * `badge_login`, so a record without it cannot be signed into whatever is presented.
 */

/** How many bytes of entropy. 24 bytes = 192 bits, well past PocketBase's 8-char floor. */
const BYTES = 24

/**
 * A random password for a cardholder nobody will ever sign in as.
 *
 * Uses the Web Crypto API rather than Math.random, which is not a CSPRNG — this value
 * does guard an account, even if only an unreachable one.
 */
export function randomCardholderPassword(): string {
  const buf = new Uint8Array(BYTES)
  crypto.getRandomValues(buf)
  // Base64 via binary string: compact, and every character is ASCII-safe for JSON.
  return btoa(String.fromCharCode(...buf))
}

/**
 * Add `password`/`passwordConfirm` to a cardholder create payload unless the caller
 * already supplied one (an operator setting an initial password in person).
 *
 * Mutates and returns `data`, so it reads as one line at the call site.
 */
export function withCardholderPassword<T extends Record<string, unknown>>(data: T): T {
  const d = data as Record<string, unknown>
  if (!d.password) {
    const generated = randomCardholderPassword()
    d.password = generated
    d.passwordConfirm = generated
  }
  return data
}
