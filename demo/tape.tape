# vhs tape for the sesh-bro demo GIF.
# Regenerate with: vhs demo/tape.tape  (requires vhs + a tty font)
Output demo/sesh-bro.gif
Set Shell bash
Set Width 120
Set Height 36
Set FontSize 14
Set Padding 12

Type "cd ~/github/herdr-sesh-bro"
Enter
Type "./sesh-bro picker"
Sleep 2s

# Move down the list to show agents
Down
Sleep 600ms
Down
Sleep 600ms
Down
Sleep 600ms

# Filter to dirs with ctrl-x, then back to all
Type "\x18"
Sleep 800ms
Type "\x0f"
Sleep 800ms

# Escape to close
Escape
Sleep 500ms

Type "echo 'alt+e (or herdr plugin action invoke open --plugin sesh-bro) opens this from anywhere'"
Enter
