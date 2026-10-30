// How the EV card's figures are counted, in one place under it instead of a caveat after each figure: what the meter can and cannot see, where the gas prices
// come from, and what the battery figures rest on. Closed by default; the figures above it do not repeat any of it.
export function EvNotes({ areaName, gas, diesel, battery }: { areaName: string | null; gas: boolean; diesel: boolean; battery: boolean }) {
  return (
    <details class="ev-notes">
      <summary>How this is counted</summary>
      <ul>
        <li>
          <strong>Energy and cost</strong> are what this meter measured. Charging somewhere else is not in them, so the cost per mile, the efficiency and the savings
          look better than they are for every mile driven on other chargers. Fixed electric fees are left out.
        </li>
        <li>
          <strong>Efficiency and cost over time</strong> are a rolling week, made from the odometer readings: the odometer is read now and then, a stretch between two
          readings is shared evenly over its days, and a point appears once a full week is behind it.
        </li>
        <li>
          <strong>A charge</strong> is a stretch in which the meter drew power. The car's battery charge is read from Ford when it begins and after it ends; the energy per
          percent is given only when the car's report came after the charge ended and the battery gained at least 2 %, so preconditioning and top-ups are not mistaken for
          charges.
        </li>
        {gas && (
          <li>
            <strong>Fuel prices</strong> are EIA's weekly average for regular gasoline{diesel ? ' and No. 2 diesel' : ''}{areaName ? ` in ${areaName}` : ''}, and each stretch is priced at the average over its
            days. The gas cars' upkeep is left out.
          </li>
        )}
        {battery && (
          <li>
            <strong>Charging efficiency</strong> is the battery's gain times its usable kWh (the figure set in Settings) over the energy the wall gave. What is lost is the
            charger, the battery's heat and keeping the battery and cabin at temperature, so it is lower in the cold.
          </li>
        )}
      </ul>
    </details>
  );
}
