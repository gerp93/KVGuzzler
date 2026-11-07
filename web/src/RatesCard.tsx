import { useEffect, useState } from 'preact/hooks';
import { api } from './api';
import { CurrentRate } from './TariffPicker';
import { routePath } from './route';
import { PriceHistory } from './PriceHistory';
import { SUSPICIOUSLY_HIGH_RATE, currencySymbol, setCurrencyCode, type RateConfig, type TariffInfo } from './lib';

export function RatesCard() {
  const [cfg, setCfg] = useState<RateConfig>();
  const [message, setMessage] = useState('');
  const [saved, setSaved] = useState(false);
  const [tariff, setTariff] = useState<TariffInfo>();

  useEffect(() => {
    api.rates().then(setCfg, (e: Error) => setMessage(e.message));
    api.tariff().then(setTariff, () => undefined);
  }, []);
  if (!cfg) return <section class="card page-narrow"><h2>Electricity price</h2><p class="note">{message || 'Loading…'}</p></section>;

  const change = (next: RateConfig) => {
    setCfg(next);
    setSaved(false);
    setMessage('');
  };
  const rate = Number(cfg.fixed_rate);
  const looksLikeCents = Number.isFinite(rate) && rate > SUSPICIOUSLY_HIGH_RATE;

  const save = () =>
    api.setRates(cfg).then(
      (r) => {
        setCfg(r);
        setCurrencyCode(r.currency);
        setSaved(true);
        setMessage('');
        api.tariff().then(setTariff, () => undefined);
      },
      (e: Error) => setMessage(e.message),
    );

  return (
    <section class="card page-narrow">
      <h2>Electricity price</h2>
      <label class="field">
        <span>Price from</span>
        <select value={cfg.mode} onChange={(e) => change({ ...cfg, mode: (e.target as HTMLSelectElement).value as RateConfig['mode'] })}>
          <option value="one">One rate</option>
          <option value="bills">Bills</option>
          <option value="lookup">A utility's published rate</option>
        </select>
      </label>

      {cfg.mode === 'lookup' && (
        tariff ? (
          <>
            <CurrentRate current={tariff} />
            <p class="caption">
              <a href={routePath('connectors')}>Change the rate in the Connectors tab</a>
            </p>
          </>
        ) : (
          <p class="note">
            No rate chosen. <a href={routePath('connectors')}>Choose one in the Connectors tab</a>.
          </p>
        )
      )}

      <label class="field">
        <span>Currency (three-letter code)</span>
        <input
          maxLength={3}
          placeholder="USD"
          value={cfg.currency ?? 'USD'}
          onInput={(e) => change({ ...cfg, currency: (e.target as HTMLInputElement).value.toUpperCase() })}
        />
      </label>

      {cfg.mode === 'one' && (
        <>
          <label class="field">
            <span>{`Rate (${currencySymbol(cfg.currency).trim()} per kWh)`}</span>
            <input
              inputMode="decimal"
              placeholder="0.15"
              value={cfg.fixed_rate ?? ''}
              onInput={(e) => {
                const v = (e.target as HTMLInputElement).value.trim();
                const { fixed_rate: _drop, ...rest } = cfg;
                change(v ? { ...rest, fixed_rate: v } : rest);
              }}
            />
          </label>
          {looksLikeCents && <p class="note">Over 1.00 per kWh. Did you enter cents? 15¢ is 0.15.</p>}
        </>
      )}

      {cfg.mode === 'bills' && (
        <p class="note">
          Prices come from the bills. <a href={routePath('history')}>Add them in the History tab</a>.
        </p>
      )}

      <div class="chips save">
        <button type="button" aria-pressed="true" onClick={() => void save()}>
          Save price
        </button>
        {saved && <span class="note">Saved.</span>}
      </div>
      {message && <p class="note error" role="alert">{message}</p>}
      <PriceHistory mode={cfg.mode} />
    </section>
  );
}
