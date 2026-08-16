package guard

import "regexp"

// classifyAction 从 Bash 命令内容推断动作类别（P6.1，借鉴 VulnClaw constraint_policy）：
// "recon"（侦察）/ "scan"（扫描）/ "exploit"（利用）。用于审计可观测 + 可选的
// DenyExploit 门控（recon-only 任务禁止利用类命令）。
func classifyAction(cmd string) string {
	if reExploit.MatchString(cmd) {
		return "exploit"
	}
	if reScan.MatchString(cmd) {
		return "scan"
	}
	return "recon"
}

// reExploit 匹配明显的"利用/持久化/webshell 写入"动作。
var reExploit = regexp.MustCompile(`(?i)` +
	`(\bmsfconsole\b|\bmetasploit\b|\bsearchsploit\b.*\bexploit\b)` +
	`|(\bsqlmap\b[^\n]*(--os-shell|--sql-shell|--dbs|--dump))` +
	`|(\bmeterpreter\b|\bmsfvenom\b)` +
	`|(\bhydra\b|\bmedusa\b|\bjohn\b|\bhashcat\b)` + // 爆破也算重动作，但常是授权的 → 单独记为 exploit 以便审计
	`|(>\s*(/var/www|/usr/share/nginx|/srv/www|/app)\S*\.(php|jsp|asp|aspx|sh))` + // webshell 写 webroot
	`|(\becho\b[^\n]*(\$\{IFS\}|\$IFS)[^\n]*(>|>>)\s*/var/www)` +
	`|(\bcurl\b[^\n]*(--data|--data-binary|--upload-file)[^\n]*\b(etc/shadow|etc/passwd))` +
	`|(\bnc\b[^\n]*(\-e\s*/bin/(sh|bash)))`)

// reScan 匹配扫描/目录爆破/指纹等主动探测动作。
var reScan = regexp.MustCompile(`(?i)` +
	`(\bnmap\b|\bmasscan\b|\bnuclei\b|\bnikto\b|\bffuf\b|\bgobuster\b|\bdirsearch\b|\bdirb\b|\bwfuzz\b)` +
	`|(\bsqlmap\b|\bxsstrike\b|\bdalfox\b|\bcommix\b)` +
	`|(\bhydra\b|\bmedusa\b)` +
	`|(\bwhatweb\b|\bwappalyzer\b|\bhttpx\b|\bsubfinder\b|\bdnsx\b|\bamass\b|\bassetfinder\b)`)

// reExfilHard 是比原 reExfil 更宽的外泄检测：编码后管道外发、curl 上传本地敏感文件等。
var reExfilHard = regexp.MustCompile(`(?i)` +
	`(\bbase64\b[^\n]*\|\s*(curl|wget|nc|ncat))` +
	`|((cat|head|tail|dd|strings)\s+/?(etc/shadow|etc/passwd|\.ssh/|\.aws/|\.env)[^\n]*\|\s*(curl|wget|nc|ncat|tee))` +
	`|(\bcurl\b[^\n]*\b(etc/shadow|etc/passwd)\b)` +
	`|((nc|ncat)\s+[^\n]*\s+<(etc/shadow|etc/passwd))`)

// reInjectionMarker 检测疑似提示注入的内容片段（P6.2 参考信号）。
var reInjectionMarker = regexp.MustCompile(`(?i)(ignore (all )?(previous|prior|above) (instructions|prompt|rules|context)|` +
	`you are now |system prompt|disregard (previous|your) instructions|` +
	`(你是|从现在开始|忽略(之前的|上面)|不要理(前面的|以上)|忘记(你的|之前的))` +
	`|do not (mention|tell|reveal)|skip (your|the) instructions)`)

// HasInjectionMarker 报告结果文本是否含疑似提示注入片段（供审计/提示层引用）。
func HasInjectionMarker(text string) bool { return reInjectionMarker.MatchString(text) }
