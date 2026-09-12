import AppKit

// Never send Quit to an installed copy selected by bundle identifier.
guard CommandLine.arguments.count == 3,
      let pid = Int32(CommandLine.arguments[1]),
      let app = NSRunningApplication(processIdentifier: pid),
      let bundle = app.bundleURL,
      bundle.resolvingSymlinksInPath().path == URL(fileURLWithPath: CommandLine.arguments[2]).resolvingSymlinksInPath().path,
      app.terminate() else {
    fputs("Could not terminate the exact candidate application.\n", stderr)
    exit(1)
}
