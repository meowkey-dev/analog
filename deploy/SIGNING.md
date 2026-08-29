# Signing the desktop app

## Where things stand

The build produces a **valid ad-hoc signature**: stable identifier `ai.xuk.analog`,
hardened runtime enabled, `codesign --verify --deep --strict` passes.

Gatekeeper still refuses it, and that is not a bug to be worked around. An ad-hoc
signature proves the bundle has not been altered since it was signed; it says
nothing about *who* signed it. Gatekeeper cares only about the second thing. There is
no local flag, entitlement or config that makes an unidentified app pass — the check
exists precisely to stop that.

So there are two honest options: remove the quarantine flag on machines you control,
or get a Developer ID.

## Right now, on your own machine

An app you built yourself is quarantined only because it arrived via a browser.

```bash
xattr -dr com.apple.quarantine /Applications/Analog.app
```

It opens normally after that, and stays that way until you download a new copy.
Right-click → Open works too, once per version.

Do this only for builds you produced or downloaded from your own CI. It is the same
gesture an attacker would like you to perform on their behalf, so the source is the
whole question.

## Properly: Developer ID + notarization

Needs a paid **Apple Developer Program** membership (currently 99 USD/year). Nothing
else grants a Developer ID, and no free tier substitutes.

**1. Create the certificate.** In Xcode → Settings → Accounts → Manage Certificates,
add a *Developer ID Application* certificate. Then export it from Keychain Access as
a `.p12` with a password.

**2. Base64 it** for GitHub:

```bash
base64 -i DeveloperID.p12 | pbcopy
```

**3. Add the repository secrets** (Settings → Secrets and variables → Actions):

| secret | what it is |
|---|---|
| `APPLE_CERTIFICATE` | the base64 `.p12` from step 2 |
| `APPLE_CERTIFICATE_PASSWORD` | the password you set when exporting |
| `APPLE_SIGNING_IDENTITY` | e.g. `Developer ID Application: Kai Xu (TEAMID)` |
| `APPLE_ID` | the Apple ID on the developer account |
| `APPLE_PASSWORD` | an **app-specific password**, made at appleid.apple.com — never the account password |
| `APPLE_TEAM_ID` | the 10-character team id |

`.github/workflows/build-app.yml` already reads all six. Absent, it skips the
keychain import and falls back to ad-hoc, so the build stays green either way. Once
they are present, the workflow imports the certificate, signs, and notarizes; the
"Report what the bundle is actually signed with" step prints the resulting authority
and Gatekeeper verdict, so you can see whether it worked rather than guess.

**These are your credentials.** Add them through the GitHub UI yourself — nobody
else needs to see them, and an app-specific password is revocable from
appleid.apple.com if one leaks.

Signing locally instead, once the certificate is in your keychain:

```bash
cd app && APPLE_SIGNING_IDENTITY="Developer ID Application: ..." npm run build
```

## Windows

Not wired, and I would not pretend otherwise. Authenticode needs a certificate from
a commercial CA, and since 2023 the key must live on hardware or in an approved
cloud HSM, which makes the CI story specific to whichever CA you buy from. The
`.msi` and NSIS `.exe` build and run; SmartScreen warns on first launch.

If you get one, Tauri signs via `bundle.windows.certificateThumbprint` with the
certificate in the machine store, or `bundle.windows.signCommand` for an HSM.

## Verifying any build

```bash
codesign -dv --verbose=2 /Applications/Analog.app   # who signed it
codesign --verify --deep --strict /Applications/Analog.app   # is it intact
spctl -a -vv /Applications/Analog.app               # would Gatekeeper allow it
```

`Signature=adhoc` with `TeamIdentifier=not set` is the unsigned-in-practice case.
A notarized build shows `Authority=Developer ID Application: ...` and `spctl`
reports `accepted` with `source=Notarized Developer ID`.
