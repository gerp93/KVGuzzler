using System.Globalization;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Layout;
using Avalonia.Media;
using VisualAssault;

namespace KVGuzzler.Views;

/// <summary>Colours for bars and charts, taken from the active theme so every theme styles them. Rows are rebuilt
/// every few seconds, so a theme change shows up on the next refresh.</summary>
internal static class UiColors
{
    public static IBrush Brush(VaToken token, double opacity = 1) =>
        new SolidColorBrush(ThemeManager.Current?[token] ?? Colors.Gray, opacity);

    /// <summary>The CPU, the GPU and everything else keep the same colour in the chart, the live bars and the program lists.</summary>
    public static IBrush Cpu => Brush(VaToken.AccentBlue);
    public static IBrush Gpu => Brush(VaToken.AccentGreen);
    public static IBrush Rest => Brush(VaToken.TextMuted, 0.6);
    public static IBrush Programs => Brush(VaToken.AccentBlue);
    public static IBrush NoProgram => Brush(VaToken.TextMuted, 0.6);

    /// <summary>"37%", or "&lt;1%" for a sliver, so a small part never reads as zero.</summary>
    public static string Percent(double share) =>
        share <= 0 ? "0%" : share < 0.005 ? "<1%" : (share * 100).ToString("0", CultureInfo.CurrentCulture) + "%";
}

/// <summary>One line of a list on the Dashboard or Programs tab: a name, a value (energy or watts), an optional price and
/// proportion bar, and a smaller line of detail.</summary>
/// <param name="Name">What the line is about: a period, a program.</param>
/// <param name="Value">The amount, e.g. "0.5 kWh" or "137.5 W".</param>
/// <param name="Cost">What it cost, e.g. "$0.10"; empty when no price is known.</param>
/// <param name="Detail">The smaller line under it.</param>
/// <param name="Tag">What the value measures, e.g. "whole PC", shown beside the name.</param>
/// <param name="Note">A caveat, e.g. "price estimated" or "still running", shown beside the name.</param>
/// <param name="Share">The line's part of the whole (0 to 1), drawn as a bar; negative for no bar.</param>
/// <param name="Bar">The bar's colour.</param>
public sealed record HistoryRow(string Name, string Value, string Cost, string Detail, string Tag = "", string Note = "",
    double Share = -1, IBrush? Bar = null)
{
    /// <summary>The whole line as one sentence (also what a screen reader and the tests see).</summary>
    public string Title => Name + ": " + Value + (Tag.Length > 0 ? " " + Tag : "") + (Cost.Length > 0 ? ", " + Cost : "")
                           + (Note.Length > 0 ? " (" + Note + ")" : "");

    public bool HasBar => Share >= 0;
    public bool HasCost => Cost.Length > 0;
    public bool HasDetail => Detail.Length > 0;
    public bool HasQualifier => Qualifier.Length > 0;
    public string Qualifier => string.Join(" · ", new[] { Tag, Note }.Where(s => s.Length > 0));
    public string ShareText => Share < 0 ? "" : UiColors.Percent(Share);
    public IBrush BarBrush => Bar ?? UiColors.Programs;
}

/// <summary>One line of the live power list. Breakdown lines are indented and dimmed: they are part of a
/// component above them, not added to the total. The share is the line's part of the whole PC (the wall
/// meter) or, with no wall meter, of the active total.</summary>
public sealed record ReadingRow(string Name, string WattsText, Thickness Indent, double Opacity,
    double Share = 0, string ShareText = "", IBrush? Bar = null)
{
    public IBrush BarBrush => Bar ?? UiColors.Cpu;
}

/// <summary>A stacked bar that splits a whole into coloured parts, with a legend underneath. Drawn from ordinary
/// controls so it follows the theme and the window width.</summary>
internal static class SplitBar
{
    public readonly record struct Part(string Label, double Value, IBrush Brush);

    public static void Fill(Grid bar, WrapPanel legend, IReadOnlyList<Part> parts)
    {
        bar.Children.Clear();
        bar.ColumnDefinitions.Clear();
        legend.Children.Clear();

        var total = parts.Sum(p => Math.Max(p.Value, 0));
        var shown = parts.Where(p => p.Value > 0).ToList();
        bar.IsVisible = legend.IsVisible = total > 0;
        if (total <= 0) return;

        for (var i = 0; i < shown.Count; i++)
        {
            bar.ColumnDefinitions.Add(new ColumnDefinition(shown[i].Value, GridUnitType.Star));
            var segment = new Border { Background = shown[i].Brush, Margin = new Thickness(0, 0, i < shown.Count - 1 ? 2 : 0, 0) };
            Grid.SetColumn(segment, i);
            bar.Children.Add(segment);
        }

        foreach (var part in parts.Where(p => p.Value > 0))
        {
            var item = new StackPanel { Orientation = Orientation.Horizontal, Spacing = 6, Margin = new Thickness(0, 0, 16, 0) };
            item.Children.Add(new Border { Width = 10, Height = 10, CornerRadius = new CornerRadius(5), Background = part.Brush, VerticalAlignment = VerticalAlignment.Center });
            item.Children.Add(new TextBlock { Text = $"{part.Label} {UiColors.Percent(part.Value / total)}", FontSize = 12 });
            legend.Children.Add(item);
        }
    }
}
