using System.Globalization;
using Avalonia;
using Avalonia.Automation;
using Avalonia.Controls;
using Avalonia.Controls.Documents;
using Avalonia.Input;
using Avalonia.Media;
using KVGuzzler.Core.History;
using VisualAssault;

namespace KVGuzzler.Views;

/// <summary>The arithmetic behind the chart (scales, tick marks, where lines break), kept free of drawing so it can be tested.</summary>
internal static class ChartLayout
{
    /// <summary>The top of the vertical scale: the next 1, 2, 5 or 10 times a power of ten above the largest value, at least 10 W.</summary>
    public static double NiceMax(double max)
    {
        if (!(max > 0)) return 10;
        var magnitude = Math.Pow(10, Math.Floor(Math.Log10(max)));
        var fraction = max / magnitude;
        var nice = fraction <= 1 ? 1 : fraction <= 2 ? 2 : fraction <= 5 ? 5 : 10;
        return Math.Max(nice * magnitude, 10);
    }

    public static IReadOnlyList<double> YTicks(double niceMax, int steps = 4) =>
        Enumerable.Range(0, steps + 1).Select(i => niceMax * i / steps).ToList();

    private static readonly TimeSpan[] TickSteps =
    {
        TimeSpan.FromMinutes(1), TimeSpan.FromMinutes(2), TimeSpan.FromMinutes(5), TimeSpan.FromMinutes(10),
        TimeSpan.FromMinutes(15), TimeSpan.FromMinutes(30), TimeSpan.FromHours(1), TimeSpan.FromHours(2),
        TimeSpan.FromHours(3), TimeSpan.FromHours(6), TimeSpan.FromHours(12), TimeSpan.FromDays(1),
        TimeSpan.FromDays(2), TimeSpan.FromDays(7),
    };

    /// <summary>Tick marks on round local times (on the hour, every six hours, at midnight), at most <paramref name="maxTicks"/> of them.</summary>
    public static IReadOnlyList<DateTimeOffset> TimeTicks(DateTimeOffset from, DateTimeOffset to, int maxTicks = 6)
    {
        var span = to - from;
        var step = TickSteps.FirstOrDefault(s => span / s <= maxTicks);
        if (step == default) step = TickSteps[^1];

        var localFrom = from.ToLocalTime();
        var origin = new DateTimeOffset(localFrom.Date, localFrom.Offset);
        var first = origin + step * Math.Ceiling((from - origin) / step);

        var ticks = new List<DateTimeOffset>();
        for (var t = first; t <= to; t += step) ticks.Add(t);
        return ticks;
    }

    /// <summary>A short label for a time on the horizontal axis: the clock for a day or so, the weekday for a week, the date beyond.</summary>
    public static string TimeLabel(DateTimeOffset t, TimeSpan span)
    {
        var local = t.ToLocalTime();
        return span <= TimeSpan.FromHours(36) ? local.ToString("HH:mm", CultureInfo.CurrentCulture)
             : span <= TimeSpan.FromDays(8) ? local.ToString("ddd HH:mm", CultureInfo.CurrentCulture)
             : local.ToString("MMM d", CultureInfo.CurrentCulture);
    }

    /// <summary>Splits a line into pieces wherever nothing was recorded for longer than a bucket and a half, so a
    /// gap in the data shows as a gap in the line instead of a misleading slope across it.</summary>
    public static IReadOnlyList<IReadOnlyList<int>> Segments(IReadOnlyList<ChartPoint> points, TimeSpan bucket)
    {
        var segments = new List<IReadOnlyList<int>>();
        var current = new List<int>();
        for (var i = 0; i < points.Count; i++)
        {
            if (i > 0 && points[i].Time - points[i - 1].Time > bucket * 1.5)
            {
                segments.Add(current);
                current = new List<int>();
            }
            current.Add(i);
        }
        if (current.Count > 0) segments.Add(current);
        return segments;
    }

    /// <summary>The index of the point nearest in time to <paramref name="t"/>, or -1 if there are none.</summary>
    public static int Nearest(IReadOnlyList<ChartPoint> points, DateTimeOffset t)
    {
        var best = -1;
        var bestDistance = TimeSpan.MaxValue;
        for (var i = 0; i < points.Count; i++)
        {
            var d = (points[i].Time - t).Duration();
            if (d < bestDistance) { best = i; bestDistance = d; }
        }
        return best;
    }
}

/// <summary>
/// Power over time: one line per series (whole PC, CPU, GPU) as average watts, with the stretches nothing was
/// recorded shaded by reason, and a readout of the values under the pointer. Drawn directly, so it adds no
/// charting library.
/// </summary>
public sealed class PowerChartControl : Control
{
    public static readonly StyledProperty<PowerChartData?> DataProperty =
        AvaloniaProperty.Register<PowerChartControl, PowerChartData?>(nameof(Data));

    // The legend takes a strip above the plot, so it never covers a line.
    private const double PreferredHeight = 300;
    private const double LeftMargin = 52, RightMargin = 12, TopMargin = 34, BottomMargin = 28;

    private Point? _hover;

    static PowerChartControl() => AffectsRender<PowerChartControl>(DataProperty);

    public PowerChartControl()
    {
        AutomationProperties.SetName(this, "Power over time");
        Focusable = false;
    }

    public PowerChartData? Data
    {
        get => GetValue(DataProperty);
        set => SetValue(DataProperty, value);
    }

    protected override Size MeasureOverride(Size availableSize) =>
        new(double.IsInfinity(availableSize.Width) ? 480 : availableSize.Width, PreferredHeight);

    protected override void OnPointerMoved(PointerEventArgs e)
    {
        base.OnPointerMoved(e);
        _hover = e.GetPosition(this);
        InvalidateVisual();
    }

    protected override void OnPointerExited(PointerEventArgs e)
    {
        base.OnPointerExited(e);
        _hover = null;
        InvalidateVisual();
    }

    // Colors come from the active VisualAssault theme, so the chart follows the user's choice.
    private static Color Token(VaToken token) => ThemeManager.Current?[token] ?? Colors.Gray;

    /// <summary>The extra meters share one colour, so each is told apart by how its line is dashed.</summary>
    private static Pen PenFor(ChartSeries series, double thickness) => new(
        BrushFor(series.Kind), thickness,
        dashStyle: series.Kind == ChartSeriesKind.Peripheral
            ? (series.Variant % 3) switch { 0 => DashStyle.Dash, 1 => DashStyle.Dot, _ => DashStyle.DashDot }
            : null,
        lineCap: series.Kind == ChartSeriesKind.Peripheral ? PenLineCap.Flat : PenLineCap.Round,
        lineJoin: PenLineJoin.Round);

    private static IBrush BrushFor(ChartSeriesKind kind) => new SolidColorBrush(Token(kind switch
    {
        // The whole PC is the brightest line of the theme: PrimaryAction is a button fill and vanishes on a dark background.
        ChartSeriesKind.Wall => VaToken.Text,
        ChartSeriesKind.Cpu => VaToken.AccentBlue,
        ChartSeriesKind.Peripheral => VaToken.TextMuted,
        _ => VaToken.AccentGreen,
    }));

    private static IBrush BandFor(GapReason reason) => new SolidColorBrush(Token(reason switch
    {
        GapReason.Asleep => VaToken.AccentMuted,
        GapReason.Off => VaToken.TextMuted,
        _ => VaToken.AccentRed,
    }), reason == GapReason.LostPower ? 0.16 : 0.14);

    private static string ReasonName(GapReason reason) => reason switch
    {
        GapReason.Asleep => "asleep",
        GapReason.Off => "off",
        _ => "lost power or crashed",
    };

    protected override void OnAttachedToVisualTree(VisualTreeAttachmentEventArgs e)
    {
        base.OnAttachedToVisualTree(e);
        ThemeManager.ThemeChanged += OnThemeChanged;
    }

    protected override void OnDetachedFromVisualTree(VisualTreeAttachmentEventArgs e)
    {
        ThemeManager.ThemeChanged -= OnThemeChanged;
        base.OnDetachedFromVisualTree(e);
    }

    private void OnThemeChanged(object? sender, VaTheme theme) => InvalidateVisual();

    private FormattedText Text(string text, double size, IBrush brush, FontWeight weight = FontWeight.Normal) =>
        new(text, CultureInfo.CurrentCulture, FlowDirection.LeftToRight, new Typeface(FontFamily.Default, FontStyle.Normal, weight), size, brush);

    public override void Render(DrawingContext context)
    {
        base.Render(context);
        // A transparent fill over the whole control: without it the pointer only "hits" where something is drawn,
        // and the hover readout would flicker or never appear between the lines.
        context.DrawRectangle(Brushes.Transparent, null, new Rect(Bounds.Size));
        var ink = TextElement.GetForeground(this) ?? Brushes.Gray;
        var faint = new SolidColorBrush(((ink as ISolidColorBrush)?.Color ?? Colors.Gray), 0.55);
        var grid = new Pen(new SolidColorBrush(((ink as ISolidColorBrush)?.Color ?? Colors.Gray), 0.15), 1);

        var plot = new Rect(LeftMargin, TopMargin, Math.Max(Bounds.Width - LeftMargin - RightMargin, 10), Math.Max(Bounds.Height - TopMargin - BottomMargin, 10));

        if (Data is not { Series.Count: > 0 } data)
        {
            var none = Text("No history to chart yet.", 13, faint);
            context.DrawText(none, new Point(plot.X + (plot.Width - none.Width) / 2, plot.Y + (plot.Height - none.Height) / 2));
            return;
        }

        var span = data.To - data.From;
        var top = ChartLayout.NiceMax(data.Series.SelectMany(s => s.Points).Select(p => p.Watts).DefaultIfEmpty(0).Max());
        double X(DateTimeOffset t) => plot.X + plot.Width * Math.Clamp((t - data.From) / span, 0, 1);
        double Y(double watts) => plot.Bottom - plot.Height * Math.Clamp(watts / top, 0, 1);

        // Horizontal grid and the watts scale.
        foreach (var w in ChartLayout.YTicks(top))
        {
            context.DrawLine(grid, new Point(plot.X, Y(w)), new Point(plot.Right, Y(w)));
            var label = Text($"{w:0} W", 12, faint);
            context.DrawText(label, new Point(plot.X - label.Width - 6, Y(w) - label.Height / 2));
        }

        // Time scale.
        foreach (var t in ChartLayout.TimeTicks(data.From, data.To))
        {
            context.DrawLine(grid, new Point(X(t), plot.Y), new Point(X(t), plot.Bottom));
            var label = Text(ChartLayout.TimeLabel(t, span), 12, faint);
            context.DrawText(label, new Point(Math.Clamp(X(t) - label.Width / 2, 0, Bounds.Width - label.Width), plot.Bottom + 7));
        }

        // Stretches nothing was recorded: asleep, off, lost power.
        foreach (var gap in data.Gaps)
        {
            var band = BandFor(gap.Reason);
            var left = X(gap.Start);
            context.FillRectangle(band, new Rect(left, plot.Y, Math.Max(X(gap.End) - left, 1), plot.Height));
        }

        // The lines.
        foreach (var series in data.Series)
        {
            var pen = PenFor(series, series.Kind == ChartSeriesKind.Wall ? 2.5 : 2);
            foreach (var segment in ChartLayout.Segments(series.Points, data.Bucket))
            {
                if (segment.Count == 1)
                {
                    var p = series.Points[segment[0]];
                    context.DrawEllipse(BrushFor(series.Kind), null, new Point(X(p.Time), Y(p.Watts)), 2, 2);
                    continue;
                }
                var geometry = new StreamGeometry();
                using (var g = geometry.Open())
                {
                    for (var i = 0; i < segment.Count; i++)
                    {
                        var p = series.Points[segment[i]];
                        var point = new Point(X(p.Time), Y(p.Watts));
                        if (i == 0) g.BeginFigure(point, false); else g.LineTo(point);
                    }
                    g.EndFigure(false);
                }
                context.DrawGeometry(null, pen, geometry);
            }
        }

        // Legend in the strip above the plot: the lines first, then what the shaded stretches mean (only the reasons present).
        var x = plot.X;
        var legendY = plot.Y - 16;
        foreach (var series in data.Series)
        {
            context.DrawLine(PenFor(series, series.Kind == ChartSeriesKind.Peripheral ? 2 : 3), new Point(x, legendY), new Point(x + 16, legendY));
            var name = Text(series.Name, 12, ink);
            context.DrawText(name, new Point(x + 22, legendY - name.Height / 2));
            x += 22 + name.Width + 18;
        }
        foreach (var reason in data.Gaps.Select(g => g.Reason).Distinct().OrderBy(r => r))
        {
            context.FillRectangle(BandFor(reason), new Rect(x, legendY - 6, 12, 12));
            var label = Text("not recorded: " + ReasonName(reason), 12, faint);
            context.DrawText(label, new Point(x + 18, legendY - label.Height / 2));
            x += 18 + label.Width + 18;
        }

        DrawHover(context, data, plot, X, Y, ink);
    }

    private void DrawHover(DrawingContext context, PowerChartData data, Rect plot, Func<DateTimeOffset, double> x, Func<double, double> y, IBrush ink)
    {
        if (_hover is not { } pointer || !plot.Contains(pointer)) return;

        var at = data.From + (data.To - data.From) * ((pointer.X - plot.X) / plot.Width);
        var lines = new List<(string Text, IBrush Brush)> { (at.ToLocalTime().ToString("g", CultureInfo.CurrentCulture), new SolidColorBrush(Token(VaToken.Text))) };
        foreach (var series in data.Series)
        {
            var i = ChartLayout.Nearest(series.Points, at);
            // Only readings close to the pointer: the value of a bucket an hour away is not "the value here".
            if (i < 0 || (series.Points[i].Time - at).Duration() > data.Bucket * 1.5) continue;
            var p = series.Points[i];
            lines.Add(($"{series.Name}: {p.Watts:0} W (peak {p.PeakWatts:0})", BrushFor(series.Kind)));
            context.DrawEllipse(BrushFor(series.Kind), null, new Point(x(p.Time), y(p.Watts)), 3.5, 3.5);
        }

        context.DrawLine(new Pen(ink, 1, dashStyle: DashStyle.Dash), new Point(pointer.X, plot.Y), new Point(pointer.X, plot.Bottom));
        if (lines.Count == 1) return;

        var texts = lines.Select(l => Text(l.Text, 11, l.Brush)).ToList();
        var width = texts.Max(t => t.Width) + 16;
        var height = texts.Sum(t => t.Height) + 10;
        var left = pointer.X + 12 + width > plot.Right ? pointer.X - 12 - width : pointer.X + 12;
        var box = new Rect(left, Math.Clamp(pointer.Y - height / 2, plot.Y, plot.Bottom - height), width, height);
        context.DrawRectangle(new SolidColorBrush(Token(VaToken.Surface), 0.94), new Pen(new SolidColorBrush(Token(VaToken.Border)), 1), box, 4, 4);
        var y0 = box.Y + 5;
        foreach (var t in texts)
        {
            context.DrawText(t, new Point(box.X + 8, y0));
            y0 += t.Height;
        }
    }
}
