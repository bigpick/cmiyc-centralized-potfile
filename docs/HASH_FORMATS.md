# Hash formats

The pool is deliberately format-agnostic. A "line" is opaque text, normally
`hash:plaintext`, and the server never splits it on `:`. The dedupe key is a
SHA-256 over the exact normalized bytes. This keeps the tool correct for
plaintexts that contain colons or arbitrary UTF-8.

## Formats you can upload straight from the potfile

For the crypt families that carry the contest points, the raw hashcat potfile
line already is exactly what KoreLogic wants:

- bcrypt (`$2*$`)
- md5crypt (`$1$`)
- sha256crypt (`$5$`) and sha512crypt (`$6$`)
- descrypt

For these, just point `send` at your potfile:

```sh
cmiyc send --potfile-path ~/.local/share/hashcat/hashcat.potfile
```

## Formats that need `hashcat --show`

A few network and authentication formats store a reformatted hash in the
potfile that does not match the original challenge line. For these, regenerate
canonical lines with `--show` against the original challenge file and upload
that output instead:

- WPA / WPA2 (mode 22000)
- NetNTLMv2
- Kerberos (AS-REP, TGS-REP, etc.)

```sh
hashcat -m 22000 --show --potfile-path team.potfile challenge_wpa.hash > show.txt
cmiyc send --author me --potfile-path show.txt
```

## Mixing is safe

You can upload raw-potfile lines and `--show` lines to the same pool. The server
dedupes opaquely, and KoreLogic ignores anything it cannot match, so there is no
harm in sending both. When in doubt about a network format, send the `--show`
output.

## Usernames

CMIYC challenge files sometimes include usernames, but the submission format
does not require them. Send `hash:plaintext` lines. If your `--show` output
includes extra fields, trim to the canonical `hash:plaintext` KoreLogic expects
for that mode before uploading.

## Submission format and encoding

A few facts from the CMIYC submission rules that the tooling already respects:

- Lines are `hash:plaintext`, one per line, and nothing else on the line.
- Hashcat potfile output works directly, as does John the Ripper potfile format
  and hashcat `--outfile-format=1,2` output.
- `$HEX[...]` encoded plaintexts are accepted. Because the pool treats each line
  as opaque bytes, a `$HEX[...]` plaintext passes through untouched.
- The email must be a single PGP signed and encrypted message (equivalent to
  `gpg -se`), either inline or attached to a plaintext email. Nested
  encrypt-then-sign MIME structures are silently dropped. `pull` produces exactly
  the single-blob armored artifact, so send `submission_<iter>.asc` as the body
  or as an attachment and you are compliant.
- Only new cracks are wanted. The steady-state `pull` sends only pending cracks;
  see `docs/DEPLOY.md` for why `--full` is recovery-only.

## Encrypted-file challenges

If the contest includes encrypted files and you crack a file's password, do not
submit the file-open password as a normal `hash:plaintext` crack. Open the file
and follow the contest-specific instructions inside it. This is a scoring rule,
not a tooling concern: just do not feed those passwords into `send`.
