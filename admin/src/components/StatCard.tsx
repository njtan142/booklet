import React from "react"
import { Card, CardContent } from "./ui/card"

interface StatCardProps {
  label: string
  value: number
  icon: React.ElementType
  color: string
}

export function StatCard({ label, value, icon: Icon, color }: StatCardProps) {
  return (
    <Card className="glass border-border">
      <CardContent className="pt-5 pb-4">
        <div className="flex items-center justify-between">
          <div>
            <p className="text-xs font-semibold text-muted-foreground uppercase tracking-wider">{label}</p>
            <p className={`text-3xl font-bold mt-1 ${color}`}>{value}</p>
          </div>
          <div className={`p-3 rounded-xl bg-current/10 ${color}`}>
            <Icon className="h-5 w-5" />
          </div>
        </div>
      </CardContent>
    </Card>
  )
}
