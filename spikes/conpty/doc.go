// Command conpty is the Windows spike of task C0.7: it runs programs on ConPTY pseudo consoles
// and compares named pipes with AF_UNIX sockets for the server's IPC, and prints the evidence
// as Markdown. Its test runs on CI's windows-latest and writes the same evidence to the file
// named by CONPTY_EVIDENCE. Decisions E12 and E13 come from that evidence.
package main
