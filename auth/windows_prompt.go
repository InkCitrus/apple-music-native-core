package auth

import "strings"

func psLiteral(value string) string { return "'" + strings.ReplaceAll(value, "'", "''") + "'" }

func (system SystemPrompt) windowsForm(code bool) string {
	text := labels(system.Language)
	fields := `$label1=New-Object Windows.Forms.Label;$label1.Text=@@account@@;$label1.Location=New-Object Drawing.Point(24,119);$label1.AutoSize=$true;$f.Controls.Add($label1);$u=New-Object Windows.Forms.TextBox;$u.Location=New-Object Drawing.Point(24,142);$u.Width=372;$f.Controls.Add($u);$label2=New-Object Windows.Forms.Label;$label2.Text=@@password@@;$label2.Location=New-Object Drawing.Point(24,181);$label2.AutoSize=$true;$f.Controls.Add($label2);$p=New-Object Windows.Forms.TextBox;$p.Location=New-Object Drawing.Point(24,204);$p.Width=372;$p.UseSystemPasswordChar=$true;$f.Controls.Add($p);`
	result := `ConvertTo-Json -Compress -InputObject @($u.Text,$p.Text)`
	title, description, button := text["connect_title"], text["description"], text["connect"]
	if code {
		title, description, button = text["verify_title"], text["verify_description"], text["verify"]
		fields = `$label1=New-Object Windows.Forms.Label;$label1.Text=@@code@@;$label1.Location=New-Object Drawing.Point(24,125);$label1.AutoSize=$true;$f.Controls.Add($label1);$u=New-Object Windows.Forms.TextBox;$u.Location=New-Object Drawing.Point(24,153);$u.Width=372;$u.MaxLength=6;$u.Font=New-Object Drawing.Font('Consolas',16);$f.Controls.Add($u);`
		result = `[Console]::WriteLine($u.Text)`
	}
	template := `[Console]::OutputEncoding=New-Object System.Text.UTF8Encoding($false);Add-Type -AssemblyName System.Windows.Forms;Add-Type -AssemblyName System.Drawing;$f=New-Object Windows.Forms.Form;$f.Text=@@client@@;$f.ClientSize=New-Object Drawing.Size(420,330);$f.Font=New-Object Drawing.Font('Segoe UI',9.5);$f.FormBorderStyle='FixedDialog';$f.MaximizeBox=$false;$f.MinimizeBox=$false;$f.StartPosition='CenterScreen';$l=New-Object Windows.Forms.Label;$l.Text=@@title@@;$l.Font=New-Object Drawing.Font('Segoe UI',16,[Drawing.FontStyle]::Bold);$l.Location=New-Object Drawing.Point(24,20);$l.Size=New-Object Drawing.Size(372,36);$f.Controls.Add($l);$d=New-Object Windows.Forms.Label;$d.Text=@@description@@;$d.Location=New-Object Drawing.Point(24,66);$d.Size=New-Object Drawing.Size(372,42);$d.ForeColor=[Drawing.Color]::DimGray;$f.Controls.Add($d);` + fields + `$hint=New-Object Windows.Forms.Label;$hint.Text=@@provider@@;$hint.Location=New-Object Drawing.Point(24,252);$hint.Size=New-Object Drawing.Size(372,18);$hint.ForeColor=[Drawing.Color]::Gray;$f.Controls.Add($hint);$cancel=New-Object Windows.Forms.Button;$cancel.Text=@@cancel@@;$cancel.Location=New-Object Drawing.Point(168,282);$cancel.Size=New-Object Drawing.Size(105,30);$cancel.DialogResult=[Windows.Forms.DialogResult]::Cancel;$f.Controls.Add($cancel);$b=New-Object Windows.Forms.Button;$b.Text=@@button@@;$b.Location=New-Object Drawing.Point(282,282);$b.Size=New-Object Drawing.Size(114,30);$b.DialogResult=[Windows.Forms.DialogResult]::OK;$f.Controls.Add($b);$f.AcceptButton=$b;$f.CancelButton=$cancel;if($f.ShowDialog() -ne [Windows.Forms.DialogResult]::OK){exit 1};` + result
	return strings.NewReplacer(
		"@@client@@", psLiteral(system.clientName()), "@@title@@", psLiteral(title),
		"@@description@@", psLiteral(strings.ReplaceAll(description, "%@", system.clientName())),
		"@@provider@@", psLiteral(strings.ReplaceAll(text["provider"], "%@", system.clientName())),
		"@@account@@", psLiteral(text["account_label"]), "@@password@@", psLiteral(text["password_label"]),
		"@@code@@", psLiteral(text["code_label"]), "@@cancel@@", psLiteral(text["cancel"]), "@@button@@", psLiteral(button),
	).Replace(template)
}
