// 跨平台打开默认浏览器。
//
// Windows : ShellExecute "open"
// macOS   : open <url>
// Linux   : xdg-open <url>

package launcher

import (
	"os/exec"
	"runtime"
)

// OpenBrowser 用系统默认浏览器打开 url。失败返回 error，不致命。
func OpenBrowser(url string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		// rundll32 url.dll,FileProtocolHandler <url>
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	case "darwin":
		cmd = exec.Command("open", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	return cmd.Start()
}
