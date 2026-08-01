import React from "react"
import {
  Dialog,
  DialogContent,
  DialogHeader,
  DialogTitle,
  DialogDescription,
  DialogFooter,
} from "../ui/dialog"
import { Button } from "../ui/button"
import { AlertTriangle, Loader2 } from "lucide-react"

type DeleteConfirmationDialogProps = {
  open: boolean
  onOpenChange: (open: boolean) => void
  count: number
  onConfirm: () => void
  isDeleting?: boolean
}

export const DeleteConfirmationDialog: React.FC<DeleteConfirmationDialogProps> = ({
  open,
  onOpenChange,
  count,
  onConfirm,
  isDeleting = false,
}) => {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className="max-w-md glass border-destructive/20">
        <DialogHeader className="gap-2">
          <div className="flex items-center gap-2 text-destructive">
            <AlertTriangle className="h-5 w-5" aria-hidden="true" />
            <DialogTitle>Delete {count} document{count === 1 ? "" : "s"}?</DialogTitle>
          </div>
          <DialogDescription className="text-xs text-muted-foreground">
            Are you sure you want to delete {count === 1 ? "this document" : `these ${count} documents`}?
            This will permanently remove the original files, split pages, and any generated booklets. This action cannot be undone.
          </DialogDescription>
        </DialogHeader>

        <DialogFooter className="mt-4 flex flex-row justify-end gap-2">
          <Button
            type="button"
            variant="outline"
            size="sm"
            onClick={() => onOpenChange(false)}
            disabled={isDeleting}
          >
            Cancel
          </Button>
          <Button
            type="button"
            variant="destructive"
            size="sm"
            onClick={() => {
              onConfirm()
              onOpenChange(false)
            }}
            disabled={isDeleting}
            className="gap-2"
          >
            {isDeleting && <Loader2 className="h-3.5 w-3.5 animate-spin" aria-hidden="true" />}
            Delete {count > 1 ? `${count} Documents` : "Document"}
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  )
}
