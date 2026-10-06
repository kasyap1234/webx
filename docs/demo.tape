# webx demo — scripted, real commands against live targets.
# Record: brew install vhs ffmpeg && vhs docs/demo.tape
# Output: docs/assets/demo.gif (referenced from README)

Output docs/assets/demo.gif

Set Shell "zsh"
Set FontSize 16
Set Width 1280
Set Height 760
Set Padding 28
Set TypingSpeed 45ms
Set PlaybackSpeed 1.4
Set Theme "Catppuccin Mocha"
Set WindowBar Colorful

Type "# webx — the web toolkit for AI coding agents"
Sleep 900ms
Enter
Sleep 400ms

Type "webx scrape https://example.com --fit 'purpose of this domain'"
Sleep 600ms
Enter
Sleep 4s
Sleep 1500ms

Type "webx search 'golang http client timeout' --depth fast"
Sleep 600ms
Enter
Sleep 9s
Sleep 1500ms

Type "webx doctor"
Sleep 600ms
Enter
Sleep 10s
Sleep 1s

Type "# one binary. scrape, search, index, extract, serve."
Sleep 2.5s
