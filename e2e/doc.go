// Package e2e holds the engine's end-to-end scenarios (TASKS.md C6.1): imux-core alone,
// driven through attached clients in PTYs. Run them with make e2e.
//
// On Windows (TASKS.md C8.8) every scenario runs, none is skipped: the clients run in ConPTY,
// the scenarios' command lines in Git for Windows' sh (IMUX_E2E_SH names another), and a
// missing server is reported as a missing endpoint (noServer). With no sh the suite says why
// and skips as a whole.
package e2e
