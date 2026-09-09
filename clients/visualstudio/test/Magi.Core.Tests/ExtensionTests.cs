using System.Text.RegularExpressions;
using Xunit;

namespace Magi.Core.Tests;

/// <summary>
/// The edges of <c>Magi.Extension</c> that nothing else measures.
/// </summary>
/// <remarks>
/// This project cannot reference that assembly — <c>LayeringTests</c> is the reason, and DESIGN §6
/// is why there is no harness for the real extension. So these read the extension as text, the same
/// way <c>WireTests</c> reads the bridge's Go source, and they guard the three things that were
/// measured to build clean and fail only once Visual Studio has it:
/// <list type="bullet">
/// <item>a <c>%key%</c> that resolves to nothing — a key that exists nowhere builds with zero
/// warnings, and the menu then draws the percent signs</item>
/// <item>the resource file left undeclared — it is not packaged, so every key is unresolved and the
/// menu draws the percent signs again, from the other direction</item>
/// <item>a binding with no property behind it — the XAML is parsed in Visual Studio's process, and
/// this project's build never sees it</item>
/// </list>
/// </remarks>
public class ExtensionTests
{
    private static string ExtensionDir()
    {
        var dir = new DirectoryInfo(AppContext.BaseDirectory);
        while (dir is not null && !Directory.Exists(Path.Combine(dir.FullName, ".git")))
        {
            dir = dir.Parent;
        }
        return Path.Combine(dir!.FullName, "clients", "visualstudio", "src", "Magi.Extension");
    }

    private static string Read(string name) => File.ReadAllText(Path.Combine(ExtensionDir(), name));

    /// <summary>
    /// Every <c>%key%</c> the extension names has a string behind it.
    /// </summary>
    /// <remarks>
    /// Measured on 2026-09-09: a key that appears in no resource file compiles, packages, and warns
    /// about nothing. The analyzer only objects to a bare literal (CEE0027), which is the opposite
    /// mistake — so nothing at build time tells a resolved key from an unresolved one, and this is
    /// the check instead.
    /// </remarks>
    [Fact]
    public void EveryResourceKeyResolves()
    {
        var resources = Read("string-resources.json");
        foreach (var file in Directory.EnumerateFiles(ExtensionDir(), "*.cs", SearchOption.AllDirectories))
        {
            foreach (var line in File.ReadLines(file))
            {
                // Code lines only. The prose above these declarations has to be able to say
                // "%key%" while explaining the trap, the same exception WireTests makes.
                var code = line.TrimStart();
                if (code.StartsWith("//", StringComparison.Ordinal) || code.StartsWith('*')) continue;
                foreach (Match m in Regex.Matches(code, @"%([A-Za-z][A-Za-z0-9_.]*)%"))
                {
                    var key = m.Groups[1].Value;
                    Assert.True(resources.Contains($"\"{key}\""),
                        $"{Path.GetFileName(file)} names %{key}%, which string-resources.json does " +
                        "not define; the menu would show the percent signs");
                }
            }
        }
    }

    /// <summary>
    /// The resource file is declared, and so ships.
    /// </summary>
    /// <remarks>
    /// It was not, and the vsix went out without it. A <c>.json</c> beside the sources is a
    /// <c>None</c> item to the SDK — nothing collects it by convention, and the build says nothing
    /// about a manifest whose every name is a key it cannot resolve.
    /// </remarks>
    [Fact]
    public void TheResourceFileIsPackaged()
    {
        Assert.Contains("string-resources.json", Read("Magi.Extension.csproj"));
    }

    /// <summary>
    /// And packaged where the shell looks: <c>.vsextension/</c>, not the root of the vsix.
    /// </summary>
    /// <remarks>
    /// Declaring the file was enough to ship it and not enough to find it. Measured 2026-09-10:
    /// with a bare <c>&lt;Content Include&gt;</c> the build was clean, the vsix carried the file at
    /// its root, <see cref="EveryResourceKeyResolves"/> was green — and the IDE drew
    /// "%Magi.OpenConversation.DisplayName%" in View &gt; Other Windows. The documented location is
    /// <c>.vsextension/string-resources.json</c> (with locale folders beside it), which
    /// <c>VSIXSubPath</c> is what puts it at. The sibling test proves the key exists; this one
    /// proves somebody can reach it.
    /// </remarks>
    [Fact]
    public void TheResourceFileIsPackagedWhereTheShellLooks()
    {
        var csproj = Read("Magi.Extension.csproj");
        var declaration = Regex.Match(csproj,
            @"<Content\s+Include=""string-resources\.json""\s*(/>|>.*?</Content>)",
            RegexOptions.Singleline);
        Assert.True(declaration.Success, "nothing in the project file includes string-resources.json");
        Assert.True(declaration.Value.Contains("<VSIXSubPath>.vsextension</VSIXSubPath>"),
            "string-resources.json is packaged without VSIXSubPath .vsextension, so it lands at the " +
            "root of the vsix where the shell does not look; every %key% would draw as itself");
    }

    /// <summary>
    /// The panel's default XAML namespace is WPF's, so its elements resolve at all.
    /// </summary>
    /// <remarks>
    /// This one is worth a test of its own because it is not a blank in a panel — it is the whole
    /// panel. Measured 2026-09-10: with the extensibility namespace as the default, Visual Studio
    /// could not create the ROOT <c>DataTemplate</c> and drew a wall of
    /// <c>XamlParseException</c> where the conversation should be. Every element written here —
    /// DataTemplate, Grid, TextBlock, Button, TextBox — is a WPF type; the 2022/xaml namespace is
    /// Remote UI's own additions and belongs on a prefix. Nothing in this repository's build parses
    /// this file, so the guard is a text one.
    /// </remarks>
    [Fact]
    public void ThePanelDeclaresTheWpfNamespaceAsItsDefault()
    {
        var xaml = Read("ConversationControl.xaml");
        var root = Regex.Match(xaml, @"<DataTemplate\b[^>]*>", RegexOptions.Singleline);
        Assert.True(root.Success, "the panel has no DataTemplate root");
        Assert.True(root.Value.Contains(@"xmlns=""http://schemas.microsoft.com/winfx/2006/xaml/presentation"""),
            "the default xmlns is not WPF's; the root element itself would fail to resolve and the " +
            "panel would be a XamlParseException. Root was: " + root.Value);
    }

    /// <summary>
    /// Every binding in the panel has a property to bind to, and that property is replicated.
    /// </summary>
    /// <remarks>
    /// Remote UI hands the XAML to Visual Studio as text and it is parsed over there, so a renamed
    /// property is not a compile error here — it is a blank in a panel, in somebody else's process.
    /// <para>
    /// Existing is only half of it. The panel binds against a proxy built from the members marked
    /// <c>DataMember</c>, and an unmarked property is not sent at all: the binding resolves to
    /// nothing and draws blank, with no error and nothing missing from the tree. Measured
    /// 2026-09-10 — a panel whose every value was empty while every element was present.
    /// </para>
    /// </remarks>
    [Fact]
    public void EveryBindingHasAPropertyBehindIt()
    {
        var model = Read("ConversationViewModel.cs");
        var xaml = Read("ConversationControl.xaml");
        var bound = Regex.Matches(xaml, @"\{Binding\s+([A-Za-z_][A-Za-z0-9_]*)")
            .Select(m => m.Groups[1].Value)
            .Distinct()
            .ToArray();
        Assert.NotEmpty(bound);
        Assert.Contains("[DataContract]", model);
        foreach (var name in bound)
        {
            var declaration = Regex.Match(model, $@"public\s+[\w\?<>,\.\[\]]+\s+{name}\b");
            Assert.True(declaration.Success,
                $"the panel binds {name}, which ConversationViewModel does not expose");
            // The attribute sits on the line above the declaration, past whatever doc comment is
            // between them. Look back at the preceding lines rather than at the whole file, so one
            // marked property cannot vouch for an unmarked one.
            var above = model[..declaration.Index].TrimEnd();
            var lastLine = above[(above.LastIndexOf('\n') + 1)..].Trim();
            Assert.True(lastLine == "[DataMember]",
                $"the panel binds {name} and ConversationViewModel does not mark it [DataMember]; " +
                "it is not replicated to the proxy the panel binds against, so it would draw blank");
        }
    }
}
