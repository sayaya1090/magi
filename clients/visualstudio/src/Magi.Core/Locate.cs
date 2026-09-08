namespace Magi.Core;

/// <summary>
/// Finding the things this client needs before it can ask anything: the config directory, and the
/// <c>magi</c> binary.
/// </summary>
/// <remarks>
/// Note what this does <b>not</b> find: the socket. Deriving a socket path is the first of the
/// eight the bridge exists to hold, both existing ports got the derivation wrong on the first try,
/// and being wrong is silent — the client looks for a socket nobody is on, says "not running", and
/// offers to start a second companion on a tree that already has one. So the bridge derives it and
/// reports it back in <c>about</c>, and nothing here recomputes it.
/// </remarks>
public static class Locate
{
    /// <summary>
    /// Where magi keeps its config — the core's three-way rule, so nobody says it twice.
    /// </summary>
    /// <remarks>
    /// Needed here despite the note above because the binary a sibling client already fetched lives
    /// under it. The rule is the core's; the parameters exist so a test can ask about a platform it
    /// is not running on.
    /// </remarks>
    public static string ConfigDir(IDictionary<string, string?>? env = null, string? home = null,
                                   PlatformID? platform = null)
    {
        string? Get(string k) => env is null
            ? Environment.GetEnvironmentVariable(k)
            : (env.TryGetValue(k, out var v) ? v : null);

        var set = Get("MAGI_CONFIG_DIR")?.Trim();
        if (!string.IsNullOrEmpty(set)) return set;

        home ??= Environment.GetFolderPath(Environment.SpecialFolder.UserProfile);
        var os = platform ?? Environment.OSVersion.Platform;
        if (os == PlatformID.Win32NT)
        {
            var appData = Get("AppData")?.Trim();
            return Path.Combine(string.IsNullOrEmpty(appData)
                ? Path.Combine(home, "AppData", "Roaming") : appData, "magi");
        }
        if (os == PlatformID.MacOSX)
        {
            return Path.Combine(home, "Library", "Application Support", "magi");
        }
        var xdg = Get("XDG_CONFIG_HOME")?.Trim();
        return Path.Combine(string.IsNullOrEmpty(xdg) ? Path.Combine(home, ".config") : xdg, "magi");
    }

    /// <summary>
    /// The binary this account already has, or null.
    /// </summary>
    /// <remarks>
    /// PATH first — an installed magi is that person's, and preferring our own copy is how the
    /// JetBrains plugin ended up fetching a second copy of a binary that was already installed.
    /// Then whatever a sibling client fetched, at ANY version rather than the one we expect: the
    /// daemon updates itself in place, so <c>bin/0.29.0/magi</c> is 0.30 a week later and insisting
    /// on our own number would re-fetch something already current.
    /// </remarks>
    public static string? Binary(string? configDir = null)
    {
        var name = OperatingSystem.IsWindows() ? "magi.exe" : "magi";
        var path = Environment.GetEnvironmentVariable("PATH") ?? "";
        foreach (var dir in path.Split(Path.PathSeparator))
        {
            if (string.IsNullOrWhiteSpace(dir)) continue;
            string candidate;
            try { candidate = Path.Combine(dir.Trim(), name); }
            catch (ArgumentException) { continue; }   // a PATH entry with characters no path can hold
            if (File.Exists(candidate)) return candidate;
        }
        var bin = Path.Combine(configDir ?? ConfigDir(), "bin");
        if (!Directory.Exists(bin)) return null;
        foreach (var version in Directory.EnumerateDirectories(bin))
        {
            var candidate = Path.Combine(version, name);
            if (File.Exists(candidate)) return candidate;
        }
        return null;
    }

    /// <summary>What to tell somebody who has no binary: one sentence, and the way to fix it.</summary>
    public const string NoBinary =
        "magi is not installed. Install it from https://github.com/sayaya1090/magi or put it on " +
        "your PATH, then run the \"magi: Start the companion for this solution\" command.";
}
