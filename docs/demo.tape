# VHS tape for the README demo GIF.
# Regenerate with: go build -o lintel ./cmd/lintel && vhs docs/demo.tape
Output docs/demo.gif

Set FontSize 15
Set Width 1100
Set Height 720
Set Padding 24
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms

Hide
Type "export PATH=$PWD:$PATH; clear"
Enter
Show

Sleep 600ms
Type "lintel check examples/demo"
Sleep 400ms
Enter
Sleep 7s
