param([Parameter(Mandatory = $true)][string]$OutputPath)
$ErrorActionPreference = 'Stop'
Add-Type -ReferencedAssemblies System.Windows.Forms,System.Drawing -OutputAssembly $OutputPath -OutputType WindowsApplication -TypeDefinition @'
using System;
using System.Drawing;
using System.Windows.Forms;
public class CherrySDKActions : Form {
    readonly Label events = new Label { AutoSize = true, Location = new Point(10, 110) };
    int clicks, wheels, keys, drags;
    bool down;
    public CherrySDKActions() {
        Text = "Cherry SDK Actions";
        ClientSize = new Size(420, 180);
        var toggle = new CheckBox { Text = "\u663e\u793a\u4fa7\u8fb9\u680f", AccessibleName = "\u663e\u793a\u4fa7\u8fb9\u680f", AutoSize = true, Location = new Point(10, 10) };
        var state = new Label { Text = "Toggle: off", AutoSize = true, Location = new Point(180, 10) };
        toggle.CheckedChanged += delegate { state.Text = toggle.Checked ? "Toggle: on" : "Toggle: off"; };
        var edit = new TextBox { AccessibleName = "Editor", Location = new Point(10, 45), Width = 380 };
        Controls.AddRange(new Control[] { toggle, state, edit, events });
        UpdateEvents();
    }
    void UpdateEvents() { events.Text = String.Format("Events: clicks={0} wheels={1} keys={2} drags={3}", clicks, wheels, keys, drags); }
    protected override void WndProc(ref Message message) {
        if (message.Msg == 0x201) down = true;
        if (message.Msg == 0x202) { clicks++; down = false; }
        if (message.Msg == 0x200 && down) drags++;
        if (message.Msg == 0x20A || message.Msg == 0x20E) wheels++;
        if (message.Msg == 0x101) keys++;
        base.WndProc(ref message);
        if (events != null && (message.Msg == 0x202 || message.Msg == 0x20A || message.Msg == 0x20E || message.Msg == 0x101)) UpdateEvents();
    }
    [STAThread] public static void Main() {
        Application.EnableVisualStyles();
        Application.Run(new CherrySDKActions());
    }
}
'@
