"use client";

interface QuickStakeProps {
  onSelect: (amount: number) => void;
  selected?: number;
}

const STAKES = [100, 500, 1000, 5000, 10000, 25000];

export default function QuickStake({ onSelect, selected }: QuickStakeProps) {
  return (
    <div className="grid grid-cols-3 gap-1.5">
      {STAKES.map((amount) => (
        <button
          key={amount}
          onClick={() => onSelect(amount)}
          className={`py-1.5 rounded text-xs font-medium transition ${
            selected === amount
              ? "bg-lotus text-white"
              : "bg-surface-lighter text-gray-300 hover:bg-gray-600"
          }`}
        >
          {"\u20B9"}
          {amount >= 1000
            ? `${(amount / 1000).toFixed(amount % 1000 === 0 ? 0 : 1)}K`
            : amount}
        </button>
      ))}
    </div>
  );
}
