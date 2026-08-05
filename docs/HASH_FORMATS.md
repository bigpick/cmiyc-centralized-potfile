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
