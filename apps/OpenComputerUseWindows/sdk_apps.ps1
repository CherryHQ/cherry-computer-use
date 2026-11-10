Add-Type -TypeDefinition @'
using System;
using System.IO;
using System.Text;
using System.Runtime.InteropServices;
public static class SDKApplicationIdentity {
    [DllImport("kernel32.dll", SetLastError = true)] static extern IntPtr OpenProcess(uint access, bool inherit, int pid);
    [DllImport("kernel32.dll")] static extern bool CloseHandle(IntPtr handle);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode)] static extern int GetApplicationUserModelId(IntPtr process, ref uint length, StringBuilder id);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)] static extern bool QueryFullProcessImageName(IntPtr process, uint flags, StringBuilder path, ref uint size);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)] static extern IntPtr CreateFile(string path, uint access, uint share, IntPtr security, uint mode, uint flags, IntPtr template);
    [DllImport("kernel32.dll", CharSet = CharSet.Unicode, SetLastError = true)] static extern uint GetFinalPathNameByHandle(IntPtr file, StringBuilder path, uint size, uint flags);

    public static string CanonicalPath(string path) {
        IntPtr file = CreateFile(path, 0, 7, IntPtr.Zero, 3, 0, IntPtr.Zero);
        if (file == new IntPtr(-1)) throw new IOException("Cannot open application executable");
        try {
            var result = new StringBuilder(32768);
            uint size = GetFinalPathNameByHandle(file, result, (uint)result.Capacity, 0);
            if (size == 0 || size >= result.Capacity) throw new IOException("Cannot resolve application executable");
            string value = result.ToString();
            if (value.StartsWith(@"\\?\UNC\")) return @"\\" + value.Substring(8);
            return value.StartsWith(@"\\?\") ? value.Substring(4) : value;
        } finally { CloseHandle(file); }
    }

    public static string ForProcess(int pid) {
        IntPtr process = OpenProcess(0x1000, false, pid);
        if (process == IntPtr.Zero) return null;
        try {
            uint length = 0;
            if (GetApplicationUserModelId(process, ref length, null) == 122) {
                var id = new StringBuilder((int)length);
                if (GetApplicationUserModelId(process, ref length, id) == 0) return "win32:aumid:" + id.ToString().ToLowerInvariant();
            }
            var path = new StringBuilder(32768);
            uint size = (uint)path.Capacity;
            if (QueryFullProcessImageName(process, 0, path, ref size)) return "win32:exe:" + CanonicalPath(path.ToString()).ToLowerInvariant();
            return null;
        } catch (IOException) { return null; }
        finally { CloseHandle(process); }
    }
}
'@

function Get-SDKAppIcon([string]$identity) {
    try {
        if ($identity.StartsWith('win32:exe:')) {
            $path = $identity.Substring(10)
            $resolved = Resolve-SDKAppIdentity $path
            if ($resolved.id -cne $identity) { return '' }
            $parsingName = $resolved.query
        } elseif ($identity.StartsWith('win32:aumid:')) {
            $resolved = Resolve-SDKAppIdentity $identity.Substring(12)
            if ($resolved.id -cne $identity) { return '' }
            $parsingName = 'shell:AppsFolder\' + $resolved.query
        } else { return '' }
        Add-Type -ReferencedAssemblies System.Drawing -TypeDefinition @'
using System;
using System.Drawing;
using System.Drawing.Imaging;
using System.IO;
using System.Runtime.InteropServices;
public static class SDKApplicationIcon {
    [StructLayout(LayoutKind.Sequential)] struct Size { public int Width, Height; }
    [ComImport, Guid("bcc18b79-ba16-442f-80c4-8a59c30c463b"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    interface ImageFactory {
        [PreserveSig] int GetImage(Size size, uint flags, out IntPtr bitmap);
    }
    [DllImport("shell32.dll", CharSet = CharSet.Unicode, PreserveSig = false)]
    static extern void SHCreateItemFromParsingName(string name, IntPtr context, ref Guid iid, out ImageFactory item);
    [DllImport("gdi32.dll")] static extern bool DeleteObject(IntPtr value);
    [StructLayout(LayoutKind.Sequential)] struct BitmapInfo {
        public uint Size; public int Width, Height; public ushort Planes, BitCount;
        public uint Compression, SizeImage; public int XPelsPerMeter, YPelsPerMeter;
        public uint ClrUsed, ClrImportant;
    }
    [DllImport("gdi32.dll")] static extern IntPtr CreateCompatibleDC(IntPtr dc);
    [DllImport("gdi32.dll")] static extern bool DeleteDC(IntPtr dc);
    [DllImport("gdi32.dll")] static extern int GetDIBits(IntPtr dc, IntPtr bitmap, uint start, uint lines, IntPtr bits, ref BitmapInfo info, uint usage);
    static Bitmap ReadBitmap(IntPtr handle) {
        using (var dimensions = Image.FromHbitmap(handle)) {
            var bitmap = new Bitmap(dimensions.Width, dimensions.Height, PixelFormat.Format32bppArgb);
            var info = new BitmapInfo { Size = 40, Width = bitmap.Width, Height = -bitmap.Height, Planes = 1, BitCount = 32 };
            IntPtr dc = CreateCompatibleDC(IntPtr.Zero);
            var bits = bitmap.LockBits(new Rectangle(0, 0, bitmap.Width, bitmap.Height), ImageLockMode.WriteOnly, PixelFormat.Format32bppArgb);
            int rows;
            try { rows = GetDIBits(dc, handle, 0, (uint)bitmap.Height, bits.Scan0, ref info, 0); }
            finally { bitmap.UnlockBits(bits); DeleteDC(dc); }
            if (rows != bitmap.Height) { bitmap.Dispose(); throw new IOException("Cannot read application icon"); }
            return bitmap;
        }
    }
    public static string Read(string name) {
        Guid iid = typeof(ImageFactory).GUID;
        ImageFactory factory;
        SHCreateItemFromParsingName(name, IntPtr.Zero, ref iid, out factory);
        try {
            IntPtr handle;
            int result = factory.GetImage(new Size { Width = 64, Height = 64 }, 4, out handle);
            if (result < 0 || handle == IntPtr.Zero) return "";
            try {
                using (var source = ReadBitmap(handle))
                using (var bitmap = new Bitmap(64, 64, PixelFormat.Format32bppArgb))
                using (var graphics = Graphics.FromImage(bitmap))
                using (var stream = new MemoryStream()) {
                    graphics.DrawImage(source, 0, 0, 64, 64);
                    bitmap.Save(stream, ImageFormat.Png);
                    if (stream.Length > 49152) return "";
                    return Convert.ToBase64String(stream.ToArray());
                }
            } finally { DeleteObject(handle); }
        } finally { Marshal.ReleaseComObject(factory); }
    }
}
'@
        return [SDKApplicationIcon]::Read($parsingName)
    } catch { return '' }
}

function Resolve-SDKAppIdentity([string]$query) {
    $script:sdkCode = 'TARGET_UNAVAILABLE'
    $query = $query.Trim()
    if ([IO.Path]::IsPathRooted($query)) {
        $path = [SDKApplicationIdentity]::CanonicalPath($query)
        if ([IO.Path]::GetExtension($path) -ine '.exe') { throw 'Expected an application executable (.exe)' }
        return @{ id = 'win32:exe:' + $path.ToLowerInvariant(); name = [IO.Path]::GetFileNameWithoutExtension($path); query = $path }
    }
    $shell = New-Object -ComObject Shell.Application
    $candidates = @{}
    foreach ($item in $shell.NameSpace('shell:AppsFolder').Items()) {
        $appID = [string]$item.Path
        $target = [string]$item.ExtendedProperty('System.Link.TargetParsingPath')
        $aliases = @($item.Name, $appID)
        if ($target) { $aliases += @($target, [IO.Path]::GetFileName($target), [IO.Path]::GetFileNameWithoutExtension($target)) }
        if ($appID -match '^Microsoft\.Windows([^_!]+)_[^!]+!') { $aliases += $Matches[1] }
        if (-not ($aliases -icontains $query)) { continue }
        if ($appID -match '^[^\\/:]+![^\\/:]+$') {
            $identity = @{ id = 'win32:aumid:' + $appID.ToLowerInvariant(); name = $item.Name; query = $appID }
        } elseif ($target -and [IO.Path]::GetExtension($target) -ieq '.exe') {
            try { $path = [SDKApplicationIdentity]::CanonicalPath($target) } catch { continue }
            $identity = @{ id = 'win32:exe:' + $path.ToLowerInvariant(); name = $item.Name; query = $path }
        } else { continue }
        $candidates[$identity.id] = $identity
    }
    if ($candidates.Count -eq 0) {
        $commands = @(Get-Command -Name $query -CommandType Application -ErrorAction SilentlyContinue | Where-Object { [IO.Path]::GetExtension($_.Source) -ieq '.exe' -and ($_.Name -ieq $query -or $_.Name -ieq ($query + '.exe')) })
        foreach ($command in $commands) {
            $path = [SDKApplicationIdentity]::CanonicalPath($command.Source)
            $identity = @{ id = 'win32:exe:' + $path.ToLowerInvariant(); name = [IO.Path]::GetFileNameWithoutExtension($path); query = $path }
            $candidates[$identity.id] = $identity
        }
    }
    if ($candidates.Count -ne 1) { throw 'Application could not be uniquely resolved; use an AppsFolder ID or executable path' }
    return @($candidates.Values)[0]
}
