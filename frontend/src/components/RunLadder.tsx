"use client";
import { useEffect, useState } from "react";
import { api } from "@/lib/api";

interface RunEntry {
  run: number;
  pnl: number;
}

export default function RunLadder({ marketId }: { marketId: string }) {
  const [ladder, setLadder] = useState<RunEntry[]>([]);

  useEffect(() => {
    api
      .request<RunEntry[]>(`/api/v1/positions/fancy/${marketId}`, { auth: true })
      .then((d) => setLadder(Array.isArray(d) ? d : []))
      .catch(() => {});
  }, [marketId]);

  if (ladder.length === 0 || ladder.every((e) => e.pnl === 0)) return null;

  return (
    <div className="bg-surface border border-gray-800/40 rounded mt-2 p-2">
      <h4 className="text-[10px] text-gray-500 font-bold uppercase mb-1">
        Run Ladder
      </h4>
      <div className="flex gap-px flex-wrap">
        {ladder.map((e) => (
          <div
            key={e.run}
            className={`text-center px-2 py-1 rounded-sm text-[10px] font-mono ${
              e.pnl > 0
                ? "bg-green-500/20 text-green-400"
                : e.pnl < 0
                ? "bg-red-500/20 text-red-400"
                : "bg-gray-800 text-gray-500"
            }`}
          >
            <div className="font-bold">{e.run}</div>
            <div>
              {e.pnl > 0 ? "+" : ""}
              {e.pnl.toFixed(0)}
            </div>
          </div>
        ))}
      </div>
    </div>
  );
}
