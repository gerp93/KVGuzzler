// Vendored-from: gerp93/VisualAssault v0.3.0 (8792ce4d2e3cacdeabb06d8042735e4fa732fc2f) - do not hand-edit; re-run scripts/update-visual-assault-avalonia.sh
#nullable enable
using System;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Media;
using Avalonia.Styling;

namespace VisualAssault;

/// <summary>
/// Applies a VisualAssault theme to the running application, and can switch
/// themes live. Resources are published two ways:
/// <list type="bullet">
/// <item>Va&lt;Token&gt;Color / Va&lt;Token&gt;Brush (e.g. VaBgBrush) for your own XAML, via DynamicResource.</item>
/// <item>Overrides of Avalonia's Fluent resource keys, so stock controls pick the theme up unstyled.</item>
/// </list>
/// </summary>
public static class ThemeManager
{
    private static ResourceDictionary? s_applied;

    public static VaTheme? Current { get; private set; }

    public static event EventHandler<VaTheme>? ThemeChanged;

    public static void Apply(string id) =>
        Apply(VisualAssaultThemes.Find(id) ?? throw new ArgumentException($"Unknown VisualAssault theme '{id}'.", nameof(id)));

    public static void Apply(VaTheme theme)
    {
        var app = Application.Current
            ?? throw new InvalidOperationException("ThemeManager.Apply needs a running Application; call it after Avalonia has initialized.");

        var dictionary = Build(theme);
        if (s_applied is not null) app.Resources.MergedDictionaries.Remove(s_applied);
        app.Resources.MergedDictionaries.Add(dictionary);
        s_applied = dictionary;

        // Controls this package does not map still get legible Fluent defaults.
        app.RequestedThemeVariant = IsDark(theme[VaToken.Bg]) ? ThemeVariant.Dark : ThemeVariant.Light;

        Current = theme;
        ThemeChanged?.Invoke(null, theme);
    }

    // Mutable SolidColorBrush on purpose: some Fluent templates bind properties
    // typed SolidColorBrush, which reject an ImmutableSolidColorBrush.
    private static ResourceDictionary Build(VaTheme theme)
    {
        var dictionary = new ResourceDictionary();

        foreach (VaToken token in Enum.GetValues(typeof(VaToken)))
        {
            var color = theme[token];
            dictionary["Va" + token + "Color"] = color;
            dictionary["Va" + token + "Brush"] = new SolidColorBrush(color);
        }

        foreach (var (key, token) in VaFluentMap.Colors)
            dictionary[key] = theme[token];
        foreach (var (key, token) in VaFluentMap.Brushes)
            dictionary[key] = new SolidColorBrush(theme[token]);

        return dictionary;
    }

    /// <summary>WCAG relative luminance below 0.5 counts as a dark background.</summary>
    internal static bool IsDark(Color color)
    {
        static double Channel(byte value)
        {
            var v = value / 255.0;
            return v <= 0.03928 ? v / 12.92 : Math.Pow((v + 0.055) / 1.055, 2.4);
        }

        return 0.2126 * Channel(color.R) + 0.7152 * Channel(color.G) + 0.0722 * Channel(color.B) < 0.5;
    }
}
