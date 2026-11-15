<!-- The conpty-evidence artifact of CI run https://github.com/hamzayogurtcuoglu/i-mux-core/actions/runs/43748534539 (commit 7c00db1),
     uploaded by the conpty-spike job. Kept as it was written. -->

# ConPTY and IPC evidence (C0.7)

Windows build: 10.0.26100

## Pseudo console flags

| What | Result |
| --- | --- |
| none | accepted |
| INHERIT_CURSOR | accepted |
| RESIZE_QUIRK | accepted |
| WIN32_INPUT_MODE | accepted |
| PASSTHROUGH_MODE | accepted |

## Output sequences, flags PASSTHROUGH_MODE

| What | Result |
| --- | --- |
| alternate screen 1049 on | passed through |
| alternate screen 1049 off | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h\x1b[?25l\x1b[H" |
| synchronized output 2026 | passed through |
| mouse 1000/1002/1003 | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h" |
| mouse SGR 1006 | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h" |
| bracketed paste 2004 | passed through |
| focus events 1004 | passed through |
| OSC 0 title | passed through |
| OSC 8 hyperlink | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h\x1b]8;id=1876-1;https://example.com…" |
| OSC 52 clipboard | passed through |
| OSC 7 cwd | passed through |
| OSC 9;9 cwd | passed through |
| kitty keys CSI > 1 u | passed through |
| modifyOtherKeys CSI > 4 ; 2 m | passed through |
| true colour 38;2 | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h\x1b[38;2;1;2;3mX\x1b[m" |
| underline style 4:3 | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h\x1b[4:3mU\x1b[24m" |
| wide characters | passed through |
| DA1 query | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h" |

## Output sequences, flags none

| What | Result |
| --- | --- |
| alternate screen 1049 on | passed through |
| alternate screen 1049 off | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h\x1b[?25l\x1b[H" |
| synchronized output 2026 | passed through |
| mouse 1000/1002/1003 | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h" |
| mouse SGR 1006 | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h" |
| bracketed paste 2004 | passed through |
| focus events 1004 | passed through |
| OSC 0 title | passed through |
| OSC 8 hyperlink | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h\x1b]8;id=6016-1;https://example.com…" |
| OSC 52 clipboard | passed through |
| OSC 7 cwd | passed through |
| OSC 9;9 cwd | passed through |
| kitty keys CSI > 1 u | passed through |
| modifyOtherKeys CSI > 4 ; 2 m | passed through |
| true colour 38;2 | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h\x1b[38;2;1;2;3mX\x1b[m" |
| underline style 4:3 | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h\x1b[4:3mU\x1b[24m" |
| wide characters | passed through |
| DA1 query | rewritten: "\x1b]0;C:\\Users\\RUNNER~1\\AppData\\Local\\Temp\\go-build4122027949\\b001\\conpty.test.exe\a\x1b[?25h" |

## Input sequences, flags PASSTHROUGH_MODE

| What | Result |
| --- | --- |
| text | passed through |
| Up | passed through |
| CSI u (C-a) | passed through |
| modifyOtherKeys (C-Enter) | passed through |
| bracketed paste | passed through |
| SGR mouse press | passed through |
| focus in | nothing read |

## Input sequences, flags none

| What | Result |
| --- | --- |
| text | passed through |
| Up | passed through |
| CSI u (C-a) | passed through |
| modifyOtherKeys (C-Enter) | passed through |
| bracketed paste | passed through |
| SGR mouse press | passed through |
| focus in | nothing read |

## Processes

| What | Result |
| --- | --- |
| cmd.exe | printed, exit 0 |
| powershell.exe | printed, exit 0 |
| pwsh.exe | printed, exit 0 |
| exit code 7 | 7 ok |
| resize 80x24 to 100x30 | start ok, resize ok, seen ok |
| kill the whole tree (job object) | TerminateJobObject ok; grandchild alive after: false |

## IPC

| What | Result |
| --- | --- |
| DACL | `D:P(A;;GA;;;S-1-5-21-3289802278-1561746829-1498425070-500)(A;;GA;;;SY)` (protected: no inherited ACEs) |
| named pipe: create with DACL and PIPE_REJECT_REMOTE_CLIENTS | ok |
| named pipe: DACL read back | `D:P(A;;FA;;;LA)(A;;FA;;;SY)` |
| named pipe: client pid → token SID | pid ok (ours: true); SID equal to ours: true ok |
| named pipe: client read after the server closed | The pipe has been ended. |
| AF_UNIX: listen | ok (59-byte path) |
| AF_UNIX: peer pid → token SID | pid ours: true; SID equal to ours: true ok |
| AF_UNIX: client read after the server closed | EOF |
| AF_UNIX: socket file DACL | error: The system cannot find the file specified. |

