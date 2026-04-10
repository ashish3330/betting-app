"use client";

/**
 * Skeleton loading primitives — dark-theme shimmer placeholders to replace
 * plain "Loading..." text. Tailored to the Lotus Exchange surface colors.
 */

import { HTMLAttributes } from "react";

function cn(...parts: Array<string | false | null | undefined>): string {
  return parts.filter(Boolean).join(" ");
}

export function Skeleton({
  className,
  ...rest
}: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "animate-pulse rounded-md bg-gray-800/60",
        className,
      )}
      {...rest}
    />
  );
}

/** Skeleton for a row of card-shaped things (markets, sports, etc.). */
export function SkeletonCardRow({ count = 3 }: { count?: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: count }).map((_, i) => (
        <div
          key={i}
          className="bg-surface border border-gray-800/60 rounded-lg p-3 space-y-2"
        >
          <div className="flex items-center justify-between">
            <Skeleton className="h-3 w-1/3" />
            <Skeleton className="h-3 w-14" />
          </div>
          <Skeleton className="h-4 w-3/4" />
          <div className="grid grid-cols-6 gap-1">
            {Array.from({ length: 6 }).map((_, j) => (
              <Skeleton key={j} className="h-9" />
            ))}
          </div>
        </div>
      ))}
    </div>
  );
}

/** Skeleton for a grid of cards (casino games, sports tiles). */
export function SkeletonGrid({
  count = 8,
  cols = "grid-cols-2 sm:grid-cols-3 lg:grid-cols-4",
  itemClassName = "h-36",
}: {
  count?: number;
  cols?: string;
  itemClassName?: string;
}) {
  return (
    <div className={cn("grid gap-3", cols)}>
      {Array.from({ length: count }).map((_, i) => (
        <Skeleton
          key={i}
          className={cn("rounded-xl border border-gray-800/60", itemClassName)}
        />
      ))}
    </div>
  );
}

/** Skeleton for the bet slip drawer placeholder. */
export function SkeletonList({ count = 5 }: { count?: number }) {
  return (
    <div className="space-y-2">
      {Array.from({ length: count }).map((_, i) => (
        <div
          key={i}
          className="bg-surface border border-gray-800/60 rounded-lg p-3 flex items-center justify-between"
        >
          <div className="space-y-1.5 flex-1">
            <Skeleton className="h-3 w-1/2" />
            <Skeleton className="h-2.5 w-2/3" />
          </div>
          <Skeleton className="h-6 w-16" />
        </div>
      ))}
    </div>
  );
}

/** Skeleton preset for the wallet/account page. */
export function SkeletonPanel() {
  return (
    <div className="space-y-4">
      <Skeleton className="h-32 rounded-xl" />
      <Skeleton className="h-20 rounded-xl" />
      <SkeletonList count={4} />
    </div>
  );
}
