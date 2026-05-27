// Package ps implements bag's drop-in for procps ps (Linux only).
//
// Supported forms / flags:
//
//	ps                       BSD short list of own pgid's processes
//	ps aux                   BSD full listing (USER PID %CPU %MEM ... CMD)
//	ps -ef                   sysv full listing (UID PID PPID ... CMD)
//	-p PID[,PID...]          show only matching pids
//	-u USER                  filter by effective user
//	--ppid PID               filter by parent
//
// Data source: /proc/<pid>/{status,stat,cmdline,comm} on Linux.
// macOS would need sysctl or libproc; we t.Skip there and emit a
// clear "ps is only supported on Linux" message.
package ps

func Main(args []string) int { return run(args) }
