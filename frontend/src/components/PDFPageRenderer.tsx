import React, { useEffect, useRef, useState } from "react";
import { Loader2 } from "lucide-react";
import { Card } from "./ui/card";

interface PDFPageRendererProps {
  url: string;
  className?: string;
  rotation?: number;
  pageNumber?: number;
}

export const PDFPageRenderer: React.FC<PDFPageRendererProps> = ({ url, className, rotation, pageNumber }) => {
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let active = true;
    setLoading(true);
    setError(null);

    const renderPDF = async () => {
      try {
        const pdfjsLib = (window as any).pdfjsLib;
        if (!pdfjsLib) {
          // Retry in 100ms if script is not fully parsed yet
          setTimeout(() => {
            if (active) renderPDF();
          }, 100);
          return;
        }

        // Configure worker
        pdfjsLib.GlobalWorkerOptions.workerSrc = "https://cdnjs.cloudflare.com/ajax/libs/pdf.js/3.4.120/pdf.worker.min.js";

        // Fetch PDF document
        const loadingTask = pdfjsLib.getDocument({
          url,
          withCredentials: true, // critical for standard cookie auth
        });
        const pdf = await loadingTask.promise;
        
        if (!active) return;

        if (pdf.numPages === 0) {
          throw new Error("No pages found in PDF");
        }

        // Render selected page (default to 1)
        const pageNum = pageNumber !== undefined ? pageNumber : 1;
        const page = await pdf.getPage(pageNum);
        if (!active) return;

        const canvas = canvasRef.current;
        if (!canvas) return;

        const context = canvas.getContext("2d");
        if (!context) return;

        // Get container dimensions to fit canvas nicely
        const rect = canvas.parentElement?.getBoundingClientRect();
        const containerWidth = rect?.width || 800;

        // Use high scale to ensure text remains crisp
        const dpr = window.devicePixelRatio || 1;
        const desiredWidth = containerWidth * dpr;
        const rotationVal = rotation !== undefined ? rotation : page.rotate;
        const baseViewport = page.getViewport({ scale: 1, rotation: rotationVal });
        const scale = desiredWidth / baseViewport.width;
        const viewport = page.getViewport({ scale: scale, rotation: rotationVal });

        canvas.width = viewport.width;
        canvas.height = viewport.height;

        // Clear canvas context
        context.clearRect(0, 0, canvas.width, canvas.height);
        
        // Render
        const renderContext = {
          canvasContext: context,
          viewport: viewport,
        };
        await page.render(renderContext).promise;
        
        if (active) {
          setLoading(false);
        }
      } catch (err: any) {
        console.error("PDF Render Error:", err);
        if (active) {
          setError("Failed to render preview. Try adjusting spacing values.");
          setLoading(false);
        }
      }
    };

    renderPDF();

    return () => {
      active = false;
    };
  }, [url, pageNumber, rotation]);

  return (
    <div className={`relative flex items-center justify-center w-full h-full bg-white ${className}`}>
      {loading && (
        <div className="absolute inset-0 flex items-center justify-center bg-white/90 z-20">
          <Loader2 className="w-5 h-5 animate-spin text-primary" />
        </div>
      )}
      {error ? (
        <div className="absolute inset-0 flex items-center justify-center p-4 bg-muted z-10">
          <Card className="p-3 text-[10px] text-destructive text-center font-medium bg-destructive/10 border-destructive/20 max-w-[85%] shadow-none">
            {error}
          </Card>
        </div>
      ) : null}
      <canvas ref={canvasRef} className="w-full h-full object-contain select-none max-h-full" />
    </div>
  );
};
