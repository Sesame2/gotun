package sysproxy

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
)

type windowsSettings struct {
	Flags                      *uint32
	Server, Bypass, AutoConfig string
}

// WinINet keeps the registry's connection data and PAC/WPAD flags consistent.
// https://learn.microsoft.com/windows/win32/wininet/setting-and-retrieving-internet-options
const windowsProxyAPI = `$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = New-Object System.Text.UTF8Encoding($false)
Add-Type -TypeDefinition @'
using System;
using System.ComponentModel;
using System.Runtime.InteropServices;
public class GoTunProxy {
    [StructLayout(LayoutKind.Sequential)] struct Options {
        public uint Size; public IntPtr Connection;
        public uint Count; public uint Error; public IntPtr Items;
    }
    public class Settings { public uint Flags; public string Server; public string Bypass; public string AutoConfig; }
    [DllImport("wininet.dll", CharSet=CharSet.Unicode, SetLastError=true)]
    static extern bool InternetQueryOptionW(IntPtr h, uint option, ref Options list, ref uint size);
    [DllImport("wininet.dll", CharSet=CharSet.Unicode, SetLastError=true)]
    static extern bool InternetSetOptionW(IntPtr h, uint option, IntPtr list, uint size);
    [DllImport("kernel32.dll")] static extern IntPtr GlobalFree(IntPtr memory);
    static Settings Exchange(Settings input) {
        int stride = IntPtr.Size == 8 ? 16 : 12;
        int valueOffset = IntPtr.Size == 8 ? 8 : 4;
        Options list = new Options { Size=(uint)Marshal.SizeOf(typeof(Options)), Count=4, Items=Marshal.AllocHGlobal(4*stride) };
        IntPtr header = IntPtr.Zero;
        for (int i=0; i<4*stride; i++) Marshal.WriteByte(list.Items, i, 0);
        try {
            string[] strings = input == null ? null : new string[] { input.Server, input.Bypass, input.AutoConfig };
            for (int i=0; i<4; i++) {
                IntPtr item = IntPtr.Add(list.Items, i*stride);
                Marshal.WriteInt32(item, i==0 && input==null ? 10 : i+1);
                if (i==0) Marshal.WriteInt32(item, valueOffset, input==null ? 0 : (int)input.Flags);
                else if (input!=null) Marshal.WriteIntPtr(item, valueOffset, Marshal.StringToHGlobalUni(strings[i-1] ?? ""));
            }
            if (input == null) {
                uint size = list.Size;
                if (!InternetQueryOptionW(IntPtr.Zero, 75, ref list, ref size)) throw new Win32Exception();
                Settings result = new Settings();
                for (int i=0; i<4; i++) {
                    IntPtr item = IntPtr.Add(list.Items, i*stride);
                    if (i==0) result.Flags=(uint)Marshal.ReadInt32(item,valueOffset);
                    else {
                        string text=Marshal.PtrToStringUni(Marshal.ReadIntPtr(item,valueOffset)) ?? "";
                        if (i==1) result.Server=text;
                        else if(i==2) result.Bypass=text;
                        else result.AutoConfig=text;
                    }
                }
                return result;
            }
            header=Marshal.AllocHGlobal((int)list.Size);
            Marshal.StructureToPtr(list, header, false);
            if (!InternetSetOptionW(IntPtr.Zero, 75, header, list.Size)) throw new Win32Exception();
            if (!InternetSetOptionW(IntPtr.Zero, 39, IntPtr.Zero, 0)) throw new Win32Exception();
            if (!InternetSetOptionW(IntPtr.Zero, 37, IntPtr.Zero, 0)) throw new Win32Exception();
            return input;
        } finally {
            for (int i=1; i<4; i++) {
                IntPtr text=Marshal.ReadIntPtr(IntPtr.Add(list.Items,i*stride),valueOffset);
                if (text!=IntPtr.Zero) {
                    if(input==null) GlobalFree(text);
                    else Marshal.FreeHGlobal(text);
                }
            }
            if (header != IntPtr.Zero) Marshal.FreeHGlobal(header);
            Marshal.FreeHGlobal(list.Items);
        }
    }
    public static Settings Query() { return Exchange(null); }
    public static void Set(uint flags, string server, string bypass, string autoConfig) {
        Exchange(new Settings { Flags=flags, Server=server, Bypass=bypass, AutoConfig=autoConfig });
    }
}
'@
`

func (m *Manager) snapshotWindows() (func() error, error) {
	output, err := m.run("powershell", "-NoProfile", "-NonInteractive", "-Command", windowsProxyAPI+"[GoTunProxy]::Query() | ConvertTo-Json -Compress")
	if err != nil {
		return nil, fmt.Errorf("读取Windows系统代理失败: %w", err)
	}
	var settings windowsSettings
	if err := json.Unmarshal(output, &settings); err != nil || settings.Flags == nil {
		return nil, fmt.Errorf("Windows代理快照格式无效")
	}
	return func() error { return m.writeWindows(settings) }, nil
}

func (m *Manager) writeWindows(settings windowsSettings) error {
	data, err := json.Marshal(settings)
	if err != nil {
		return err
	}
	script := windowsProxyAPI + "$settings = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('" + base64.StdEncoding.EncodeToString(data) + "')) | ConvertFrom-Json\n[GoTunProxy]::Set($settings.Flags, $settings.Server, $settings.Bypass, $settings.AutoConfig)"
	if _, err := m.run("powershell", "-NoProfile", "-NonInteractive", "-Command", script); err != nil {
		return fmt.Errorf("设置Windows系统代理失败: %w", err)
	}
	return nil
}

func (m *Manager) enableWindows() error {
	flags := uint32(3) // Direct + explicit proxy, without PAC or auto-detection.
	server := "http=" + m.httpAddr + ";https=" + m.httpAddr
	if m.socksAddr != "" {
		server += ";socks=" + m.socksAddr
	}
	return m.writeWindows(windowsSettings{Flags: &flags, Server: server})
}

func (m *Manager) clearWindows() error {
	flags := uint32(1)
	return m.writeWindows(windowsSettings{Flags: &flags})
}
