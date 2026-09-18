param([Parameter(Mandatory = $true)][string]$OutputPath)
$ErrorActionPreference = 'Stop'
Add-Type -ReferencedAssemblies System.Windows.Forms,System.Drawing -OutputAssembly $OutputPath -OutputType WindowsApplication -TypeDefinition @'
using System;
using System.Drawing;
using System.Windows.Forms;
public static class CherrySDKFixture {
    [STAThread] public static void Main() {
        Application.EnableVisualStyles();
        var form = new Form { Text = "Cherry SDK Fixture", ClientSize = new Size(320, 160) };
        var count = 0;
        var button = new Button { Text = "Count: 0", Dock = DockStyle.Fill };
        button.Click += delegate { button.Text = "Count: " + (++count); };
        form.Controls.Add(button);
        Application.Run(form);
    }
}
'@
