//go:build windows

package notify

import (
	"fmt"
	"os/exec"
	"strings"
	"syscall"
)

// Send displays a Windows toast notification without flashing a console window.
func Send(title, body string) {
	t := strings.ReplaceAll(title, `'`, `''`)
	b := strings.ReplaceAll(body, `'`, `''`)
	script := fmt.Sprintf(`
[Windows.UI.Notifications.ToastNotificationManager, Windows.UI.Notifications, ContentType = WindowsRuntime] > $null
$template = [Windows.UI.Notifications.ToastNotificationManager]::GetTemplateContent([Windows.UI.Notifications.ToastTemplateType]::ToastText02)
$textNodes = $template.GetElementsByTagName("text")
$textNodes.Item(0).AppendChild($template.CreateTextNode('%s')) > $null
$textNodes.Item(1).AppendChild($template.CreateTextNode('%s')) > $null
$toast = [Windows.UI.Notifications.ToastNotification]::new($template)
[Windows.UI.Notifications.ToastNotificationManager]::CreateToastNotifier("MyOVPN").Show($toast)
`, t, b)
	cmd := exec.Command("powershell", "-NoProfile", "-NonInteractive", "-WindowStyle", "Hidden", "-Command", script)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	_ = cmd.Start()
}
