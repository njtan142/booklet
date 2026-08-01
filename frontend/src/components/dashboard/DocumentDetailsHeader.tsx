import React from "react"
import type { DocumentDetail } from "../../api"

type DocumentDetailsHeaderProps = {
  docDetail: DocumentDetail
}

export const DocumentDetailsHeader: React.FC<DocumentDetailsHeaderProps> = ({ docDetail }) => (
  <div>
    <span className="text-[10px] uppercase font-bold text-primary tracking-wider">Document Details</span>
    <h2 className="text-xl font-extrabold text-foreground mt-1">{docDetail.name}</h2>
    <p className="text-muted-foreground text-xs mt-1">
      Uploaded {new Date(docDetail.created_at).toLocaleDateString()}
    </p>
  </div>
)
