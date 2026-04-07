"use client";

import { LiveScore as LiveScoreType } from "@/lib/api";

interface LiveScoreProps {
  score: LiveScoreType;
}

export default function LiveScore({ score }: LiveScoreProps) {
  return (
    <div className="bg-surface rounded-xl border border-gray-800 overflow-hidden">
      {/* Header */}
      <div className="flex items-center gap-2 px-3 py-1.5 bg-profit/10 border-b border-gray-800">
        <span className="w-1.5 h-1.5 bg-profit rounded-full animate-pulse" />
        <span className="text-[10px] text-profit font-medium uppercase tracking-wide">
          Live Score
        </span>
      </div>

      <div className="p-3">
        {/* Teams */}
        <div className="space-y-2">
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium text-white">
              {score.home}
            </span>
            <span className="text-sm font-bold text-white font-mono">
              {score.home_score}
            </span>
          </div>
          <div className="flex items-center justify-between">
            <span className="text-sm font-medium text-gray-400">
              {score.away}
            </span>
            <span className="text-sm font-bold text-gray-400 font-mono">
              {score.away_score}
            </span>
          </div>
        </div>

        {/* Cricket Stats */}
        <div className="mt-3 pt-3 border-t border-gray-800 grid grid-cols-3 gap-2">
          {score.overs && (
            <div className="text-center">
              <div className="text-[10px] text-gray-500 uppercase">Overs</div>
              <div className="text-xs font-bold text-white font-mono mt-0.5">
                {score.overs}
              </div>
            </div>
          )}
          {score.run_rate && (
            <div className="text-center">
              <div className="text-[10px] text-gray-500 uppercase">CRR</div>
              <div className="text-xs font-bold text-white font-mono mt-0.5">
                {score.run_rate}
              </div>
            </div>
          )}
          {score.required_rate && (
            <div className="text-center">
              <div className="text-[10px] text-gray-500 uppercase">RRR</div>
              <div className="text-xs font-bold text-lotus font-mono mt-0.5">
                {score.required_rate}
              </div>
            </div>
          )}
        </div>

        {/* Last Wicket / Partnership */}
        {(score.last_wicket || score.partnership) && (
          <div className="mt-2 pt-2 border-t border-gray-800 space-y-1">
            {score.partnership && (
              <div className="flex justify-between text-[10px]">
                <span className="text-gray-500">Partnership</span>
                <span className="text-gray-300 font-mono">
                  {score.partnership}
                </span>
              </div>
            )}
            {score.last_wicket && (
              <div className="flex justify-between text-[10px]">
                <span className="text-gray-500">Last Wkt</span>
                <span className="text-gray-300 font-mono">
                  {score.last_wicket}
                </span>
              </div>
            )}
          </div>
        )}
      </div>
    </div>
  );
}
