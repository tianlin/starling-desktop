using System;
using System.Runtime.InteropServices;
using System.Runtime.InteropServices.ComTypes;
using System.Text;

namespace Starling.Installer {
    // Explicit Unicode interface: WScript.Shell rejects non-codepage paths on
    // some Windows hosts. Method order follows the native IShellLinkW vtable.
    // https://learn.microsoft.com/windows/win32/api/shobjidl_core/nn-shobjidl_core-ishelllinkw
    [ComImport, Guid("00021401-0000-0000-C000-000000000046")]
    internal class ShellLinkObject { }

    [ComImport, Guid("000214F9-0000-0000-C000-000000000046"), InterfaceType(ComInterfaceType.InterfaceIsIUnknown)]
    internal interface IShellLinkW {
        void GetPath([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder path, int count, IntPtr findData, uint flags);
        void GetIDList(out IntPtr idList);
        void SetIDList(IntPtr idList);
        void GetDescription([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder text, int count);
        void SetDescription([MarshalAs(UnmanagedType.LPWStr)] string text);
        void GetWorkingDirectory([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder path, int count);
        void SetWorkingDirectory([MarshalAs(UnmanagedType.LPWStr)] string path);
        void GetArguments([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder text, int count);
        void SetArguments([MarshalAs(UnmanagedType.LPWStr)] string text);
        void GetHotkey(out short key);
        void SetHotkey(short key);
        void GetShowCmd(out int command);
        void SetShowCmd(int command);
        void GetIconLocation([Out, MarshalAs(UnmanagedType.LPWStr)] StringBuilder path, int count, out int index);
        void SetIconLocation([MarshalAs(UnmanagedType.LPWStr)] string path, int index);
        void SetRelativePath([MarshalAs(UnmanagedType.LPWStr)] string path, uint reserved);
        void Resolve(IntPtr window, uint flags);
        void SetPath([MarshalAs(UnmanagedType.LPWStr)] string path);
    }

    public static class ShellLinks {
        public static void Create(string shortcut, string target, string directory) {
            object instance = new ShellLinkObject();
            try {
                IShellLinkW link = (IShellLinkW)instance;
                link.SetPath(target);
                link.SetWorkingDirectory(directory);
                link.SetDescription("Starling - unofficial desktop podcast client");
                ((IPersistFile)instance).Save(shortcut, true);
            } finally { Marshal.FinalReleaseComObject(instance); }
        }
        public static string ReadTarget(string shortcut) {
            object instance = new ShellLinkObject();
            try {
                ((IPersistFile)instance).Load(shortcut, 0);
                StringBuilder buffer = new StringBuilder(32768);
                ((IShellLinkW)instance).GetPath(buffer, buffer.Capacity, IntPtr.Zero, 4);
                return buffer.ToString();
            } finally { Marshal.FinalReleaseComObject(instance); }
        }
    }
}
