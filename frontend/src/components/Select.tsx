"use client";

import { useState, useRef, useEffect } from "react";

export interface SelectOption {
  value: string;
  label: string;
  subtitle?: string;
}

interface SelectProps {
  options: SelectOption[];
  value: string;
  onChange: (value: string) => void;
  placeholder?: string;
  className?: string;
}

export default function Select({ options, value, onChange, placeholder = "Select...", className = "" }: SelectProps) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);

  const selected = options.find((o) => o.value === value);

  useEffect(() => {
    function handleClick(e: MouseEvent) {
      if (ref.current && !ref.current.contains(e.target as Node)) {
        setOpen(false);
      }
    }
    document.addEventListener("mousedown", handleClick);
    return () => document.removeEventListener("mousedown", handleClick);
  }, []);

  return (
    <div ref={ref} className={`relative ${className}`}>
      {/* Trigger */}
      <button
        type="button"
        onClick={() => setOpen(!open)}
        className="w-full h-9 px-3 text-left text-sm bg-[var(--bg-surface)] border border-gray-800/60 rounded-lg text-white flex items-center justify-between gap-2 hover:border-gray-700 focus:outline-none focus:border-lotus/60 transition"
      >
        <span className={selected ? "truncate" : "text-gray-500 truncate"}>
          {selected ? selected.label : placeholder}
        </span>
        <svg
          className={`w-3.5 h-3.5 text-gray-500 flex-shrink-0 transition-transform ${open ? "rotate-180" : ""}`}
          fill="none" stroke="currentColor" viewBox="0 0 24 24"
        >
          <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2} d="M19 9l-7 7-7-7" />
        </svg>
      </button>

      {/* Dropdown */}
      {open && (
        <div className="absolute z-50 top-full left-0 right-0 mt-1 bg-[var(--dropdown-bg)] border border-gray-800/60 rounded-lg shadow-xl overflow-hidden max-h-60 overflow-y-auto">
          {options.map((opt) => {
            const isSelected = opt.value === value;
            return (
              <button
                key={opt.value}
                type="button"
                onClick={() => { onChange(opt.value); setOpen(false); }}
                className={`w-full text-left px-3 py-2 text-sm flex items-center gap-2 transition ${
                  isSelected
                    ? "bg-lotus/10 text-lotus font-medium"
                    : "text-gray-300 hover:bg-white/5 hover:text-white"
                }`}
              >
                {isSelected && (
                  <svg className="w-3.5 h-3.5 text-lotus flex-shrink-0" fill="none" stroke="currentColor" viewBox="0 0 24 24">
                    <path strokeLinecap="round" strokeLinejoin="round" strokeWidth={2.5} d="M5 13l4 4L19 7" />
                  </svg>
                )}
                <div className={isSelected ? "" : "pl-5"}>
                  <div className="truncate">{opt.label}</div>
                  {opt.subtitle && (
                    <div className="text-[10px] text-gray-500 truncate">{opt.subtitle}</div>
                  )}
                </div>
              </button>
            );
          })}
          {options.length === 0 && (
            <div className="px-3 py-4 text-center text-xs text-gray-500">No options</div>
          )}
        </div>
      )}
    </div>
  );
}
