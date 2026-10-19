using System.Globalization;
using Avalonia;
using Avalonia.Controls;
using Avalonia.Layout;
using KVGuzzler.Core;
using KVGuzzler.Core.Billing;
using KVGuzzler.Core.History;
using KVGuzzler.Core.Power;
using KVGuzzler.Data;

namespace KVGuzzler.Views;

// Settings > Electricity rate > "My bills": the price per kWh comes from each bill (its dates, total and kWh), and is
// used for everything recorded on those dates.
public partial class MainWindow
{
    private int _editingBillId;
    private int _pendingDeleteId;
    private readonly List<(TextBox Label, TextBox Amount)> _feeRows = new();

    private void StartBillsUi()
    {
        RateModeBox.SelectedIndex = _services.Database?.GetSetting(SettingKeys.RateMode) == "bills" ? 1 : 0;
        ApplyRateMode();

        RateModeBox.SelectionChanged += (_, _) => OnRateModeChanged();
        AddFeeButton.Click += (_, _) => AddFeeRow();
        SaveBillButton.Click += async (_, _) => await SaveBillAsync();
        CancelBillButton.Click += (_, _) => ClearBillForm(clearStatus: true);
        BillTotalBox.TextChanged += (_, _) => UpdateBillPreview();
        BillKwhBox.TextChanged += (_, _) => UpdateBillPreview();
        BillStartPicker.SelectedDateChanged += (_, _) => UpdateBillPreview();
        BillEndPicker.SelectedDateChanged += (_, _) => UpdateBillPreview();

        _ = RefreshBillsAsync();
    }

    private bool UsingBills => RateModeBox.SelectedIndex == 1;

    private void ApplyRateMode()
    {
        OneRatePanel.IsVisible = !UsingBills;
        BillsPanel.IsVisible = UsingBills;
    }

    private void OnRateModeChanged()
    {
        ApplyRateMode();
        if (_restartRequired || _services.Database is null) return;

        _services.Database.SetSetting(SettingKeys.RateMode, UsingBills ? "bills" : "one");
        _services.Database.SetSetting(SettingKeys.Currency, Currency);
        RefreshEstimate();
        _ = RefreshBillsAsync();
        _ = RefreshCollectorAsync(includeService: false);
    }

    // ----- Reading the form -------------------------------------------------------------

    private static bool TryNumber(string? text, out decimal value)
    {
        value = 0;
        if (string.IsNullOrWhiteSpace(text)) return false;
        // Plain decimals only (no thousands separators), as for the single rate: with them "206.32" would read as 20632
        // where '.' separates thousands. The user's own separator is tried first, then a plain '.'.
        const NumberStyles style = NumberStyles.AllowDecimalPoint | NumberStyles.AllowLeadingSign;
        return decimal.TryParse(text.Trim(), style, CultureInfo.CurrentCulture, out value)
               || decimal.TryParse(text.Trim(), style, CultureInfo.InvariantCulture, out value);
    }

    /// <summary>The bill typed into the form, or null with the reason in <paramref name="error"/>.</summary>
    private Bill? ReadBillForm(out string? error)
    {
        error = null;
        if (BillStartPicker.SelectedDate is not { } start || BillEndPicker.SelectedDate is not { } end)
        {
            error = "Choose the first and last day the bill covers.";
            return null;
        }
        if (!TryNumber(BillTotalBox.Text, out var total))
        {
            error = "Enter the bill's total as a plain number, like 206.32.";
            return null;
        }
        if (!TryNumber(BillKwhBox.Text, out var kwh))
        {
            error = "Enter the kWh as a plain number, like 1036 (no thousands separator).";
            return null;
        }

        var fees = new List<FixedFee>();
        foreach (var (labelBox, amountBox) in _feeRows)
        {
            var label = labelBox.Text?.Trim() ?? "";
            if (label.Length == 0 && string.IsNullOrWhiteSpace(amountBox.Text)) continue;   // an empty row is ignored
            if (!TryNumber(amountBox.Text, out var amount))
            {
                error = $"Flat fee \"{(label.Length == 0 ? "(no name)" : label)}\": enter the amount as a number (minus for a credit).";
                return null;
            }
            fees.Add(new FixedFee(label.Length == 0 ? "Flat fee" : label, amount));
        }

        return new Bill(_editingBillId, DateOnly.FromDateTime(start), DateOnly.FromDateTime(end), total, (double)kwh, fees);
    }

    private void UpdateBillPreview()
    {
        BillWarningText.IsVisible = false;
        if (ReadBillForm(out _) is not { } bill || !(bill.Kwh > 0) || bill.Variable <= 0)
        {
            BillPriceText.Text = "";
            return;
        }

        BillPriceText.Text = $"Price per kWh: {Currency}{bill.PricePerKwh.ToString("0.0000", CultureInfo.CurrentCulture)}"
            + (bill.FixedFees.Count == 0 ? " (the total divided by the kWh)"
               : $" (after leaving out {bill.FixedFees.Count} flat fee{(bill.FixedFees.Count == 1 ? "" : "s")})");
        if (bill.PricePerKwh > Rate.SuspiciouslyHigh)
        {
            BillWarningText.Text = $"That is more than 1.00 per kWh. Check the total and the kWh (is the kWh in thousands, or the total in cents?).";
            BillWarningText.IsVisible = true;
        }
    }

    // ----- Flat fee rows ----------------------------------------------------------------

    private void AddFeeRow(string label = "", string amount = "")
    {
        var labelBox = new TextBox { Width = 230, Text = label, PlaceholderText = "Fee name, e.g. Basic service" };
        var amountBox = new TextBox { Width = 150, Text = amount, PlaceholderText = "6.00 (minus for a credit)" };
        var remove = new Button { Content = "Remove" };
        var row = new StackPanel { Orientation = Orientation.Horizontal, Spacing = 8, Children = { labelBox, amountBox, remove } };

        var entry = (labelBox, amountBox);
        _feeRows.Add(entry);
        FeesPanel.Children.Add(row);
        labelBox.TextChanged += (_, _) => UpdateBillPreview();
        amountBox.TextChanged += (_, _) => UpdateBillPreview();
        remove.Click += (_, _) =>
        {
            _feeRows.Remove(entry);
            FeesPanel.Children.Remove(row);
            UpdateBillPreview();
        };
    }

    // ----- Saving, editing, deleting ----------------------------------------------------

    internal async Task SaveBillAsync()
    {
        if (_restartRequired || _services.Database is null)
        {
            BillStatusText.Text = "Restart KVGuzzler first: the database location was changed.";
            return;
        }
        if (ReadBillForm(out var error) is not { } bill)
        {
            BillStatusText.Text = error;
            return;
        }
        if (BillValidator.Validate(bill, _services.Database.GetBills()) is { } problem)
        {
            BillStatusText.Text = problem;
            return;
        }

        var editing = bill.Id != 0;
        _services.Database.SaveBill(bill);
        _services.Database.SetSetting(SettingKeys.Currency, Currency);
        ClearBillForm(clearStatus: false);
        BillStatusText.Text = editing ? "Bill updated." : "Bill saved.";
        await RefreshBillsAsync();
        await RefreshCollectorAsync(includeService: false);
    }

    private void ClearBillForm(bool clearStatus)
    {
        _editingBillId = 0;
        BillStartPicker.SelectedDate = null;
        BillEndPicker.SelectedDate = null;
        BillTotalBox.Text = "";
        BillKwhBox.Text = "";
        _feeRows.Clear();
        FeesPanel.Children.Clear();
        BillFormTitle.Text = "Add a bill";
        SaveBillButton.Content = "Save bill";
        BillPriceText.Text = "";
        BillWarningText.IsVisible = false;
        if (clearStatus) BillStatusText.Text = "";
    }

    private void LoadBillIntoForm(Bill bill)
    {
        ClearBillForm(clearStatus: true);
        _editingBillId = bill.Id;
        BillStartPicker.SelectedDate = bill.Start.ToDateTime(TimeOnly.MinValue);
        BillEndPicker.SelectedDate = bill.End.ToDateTime(TimeOnly.MinValue);
        BillTotalBox.Text = bill.Total.ToString("0.00", CultureInfo.CurrentCulture);
        BillKwhBox.Text = bill.Kwh.ToString("0.###", CultureInfo.CurrentCulture);
        foreach (var fee in bill.FixedFees) AddFeeRow(fee.Label, fee.Amount.ToString("0.00", CultureInfo.CurrentCulture));
        BillFormTitle.Text = $"Edit the bill for {bill.Start:yyyy-MM-dd} to {bill.End:yyyy-MM-dd}";
        SaveBillButton.Content = "Save changes";
        UpdateBillPreview();
    }

    // ----- The list of bills, with the check against what KVGuzzler measured ----------------

    /// <summary>How much of a bill's kWh KVGuzzler accounts for: the whole PC from the wall meter if it recorded in those
    /// dates, otherwise the CPU and GPU sensors (active power only), otherwise nothing.</summary>
    internal static string CheckText(Bill bill, RateSchedule schedule, HistoryStore? store, string currency)
    {
        if (store is null) return "KVGuzzler has no recording for these dates.";
        var (from, to) = schedule.Span(bill);
        var wall = store.Query(from, to, ReadingScope.Wall);
        var active = store.Query(from, to);
        var length = to - from;

        string Percent(double kwh) => (kwh / bill.Kwh).ToString("P1", CultureInfo.CurrentCulture);
        string Coverage(TimeSpan monitored) =>
            monitored < length * 0.97 ? $" It was only recording for {(monitored / length).ToString("P0", CultureInfo.CurrentCulture)} of these dates, so the real share is higher." : "";

        if (wall.Monitored > TimeSpan.Zero)
            return $"KVGuzzler's wall meter measured {wall.KilowattHours.ToString("0.#", CultureInfo.CurrentCulture)} kWh of the bill's {bill.Kwh.ToString("0.###", CultureInfo.CurrentCulture)} kWh "
                 + $"({Percent(wall.KilowattHours)}), about {currency}{(wall.KilowattHours * (double)bill.PricePerKwh).ToString("N2", CultureInfo.CurrentCulture)} of the bill.{Coverage(wall.Monitored)}";
        if (active.Monitored > TimeSpan.Zero)
            return $"KVGuzzler's CPU and GPU sensors measured {active.KilowattHours.ToString("0.#", CultureInfo.CurrentCulture)} kWh ({Percent(active.KilowattHours)} of the bill's kWh), active power only; "
                 + $"a wall meter would show the whole PC.{Coverage(active.Monitored)}";
        return "KVGuzzler has no recording for these dates.";
    }

    internal async Task RefreshBillsAsync()
    {
        if (_services.Database is not { } db) return;
        try
        {
            var schedule = db.LoadRateSchedule();
            var currency = Currency;
            var bills = schedule.Bills;
            var checks = await Task.Run(() =>
            {
                var store = _services.OpenHistory();
                var result = new Dictionary<int, string>();
                foreach (var b in bills)
                {
                    try { result[b.Id] = CheckText(b, schedule, store, currency); }
                    catch (Exception e) when (e is Microsoft.Data.Sqlite.SqliteException or IOException or UnauthorizedAccessException) { result[b.Id] = ""; }
                }
                return result;
            });
            RenderBills(bills, checks, currency);
        }
        catch (Exception e) when (e is not OperationCanceledException)
        {
            BillsNoteText.Text = "Could not load the bills: " + e.Message;
        }
    }

    private void RenderBills(IReadOnlyList<Bill> bills, IReadOnlyDictionary<int, string> checks, string currency)
    {
        BillsList.Children.Clear();
        BillsNoteText.Text = bills.Count == 0 ? "No bills yet. Add one below, or switch back to One rate." : "";
        BillsNoteText.IsVisible = bills.Count == 0;

        foreach (var bill in bills.OrderByDescending(b => b.Start))
        {
            var texts = new StackPanel { Spacing = 2 };
            texts.Children.Add(new TextBlock
            {
                FontWeight = Avalonia.Media.FontWeight.SemiBold,
                TextWrapping = Avalonia.Media.TextWrapping.Wrap,
                Text = $"{bill.Start:yyyy-MM-dd} to {bill.End:yyyy-MM-dd}: {currency}{bill.Total.ToString("N2", CultureInfo.CurrentCulture)}, "
                       + $"{bill.Kwh.ToString("0.###", CultureInfo.CurrentCulture)} kWh, {currency}{bill.PricePerKwh.ToString("0.0000", CultureInfo.CurrentCulture)} per kWh",
            });
            if (bill.FixedFees.Count > 0)
                texts.Children.Add(Small("Leaves out: " + string.Join("; ", bill.FixedFees.Select(f => $"{f.Label} {f.Amount.ToString("0.00", CultureInfo.CurrentCulture)}"))));
            if (checks.TryGetValue(bill.Id, out var check) && check.Length > 0) texts.Children.Add(Small(check));

            var edit = new Button { Content = "Edit" };
            edit.Click += (_, _) => LoadBillIntoForm(bill);
            var delete = new Button { Content = _pendingDeleteId == bill.Id ? "Really delete?" : "Delete" };
            delete.Click += (_, _) =>
            {
                if (_pendingDeleteId != bill.Id)
                {
                    _pendingDeleteId = bill.Id;   // a second press confirms
                    _ = RefreshBillsAsync();
                    return;
                }
                _pendingDeleteId = 0;
                _services.Database?.DeleteBill(bill.Id);
                if (_editingBillId == bill.Id) ClearBillForm(clearStatus: true);
                BillStatusText.Text = "Bill deleted.";
                _ = RefreshBillsAsync();
                _ = RefreshCollectorAsync(includeService: false);
            };

            var buttons = new StackPanel { Orientation = Orientation.Horizontal, Spacing = 6, VerticalAlignment = VerticalAlignment.Top, Children = { edit, delete } };
            var row = new Grid { ColumnDefinitions = new ColumnDefinitions("*,Auto"), Margin = new Thickness(0, 0, 0, 4) };
            Grid.SetColumn(buttons, 1);
            row.Children.Add(texts);
            row.Children.Add(buttons);
            BillsList.Children.Add(row);
        }
    }

    private static TextBlock Small(string text) => new()
    {
        Text = text, Opacity = 0.7, FontSize = 12, TextWrapping = Avalonia.Media.TextWrapping.Wrap,
    };
}
