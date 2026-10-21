# Re-record with:   vhs docs/demo/demo.tape   (from the repository root)
# Needs vhs (https://github.com/charmbracelet/vhs) with ttyd and ffmpeg, and
# a built muster binary at ./bin/muster:   go build -o bin/muster ./cmd/muster

Output docs/demo.gif

Set Shell "bash"
Set FontFamily "DejaVu Sans Mono"
Set FontSize 15
Set Width 1200
Set Height 700
Set Padding 14
Set Theme "Catppuccin Mocha"
Set TypingSpeed 70ms
Set Framerate 15

Hide
Type "export PS1='$ ' PATH=$PWD/bin:$PATH && cd docs/demo && clear"
Enter
Show

# Start the whole stack from one file.
Type "muster"
Sleep 600ms
Enter
Sleep 4s

# Look around: each process has its own log, CPU, memory and uptime.
Type "j"
Sleep 2s
Type "j"
Sleep 2s

# payments is about to crash: watch it get caught and restarted with backoff.
Type "j"
Sleep 7s

# Search the log.
Type "/"
Sleep 400ms
Type "restart"
Sleep 800ms
Enter
Sleep 2s
Escape
Sleep 500ms

# Control one process without touching the others.
Type "kkk"
Sleep 1s
Type "s"
Sleep 2s
Enter
Sleep 2s
Type "r"
Sleep 2s

# Quit: every child process goes with it.
Type "q"
Sleep 1500ms
