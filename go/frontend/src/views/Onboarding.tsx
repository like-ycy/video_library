import { useState } from "react";
import { Button } from "xwang-ui";
import { call } from "../api";
import { useFeedback } from "../feedback";
import { EmptyState, PageHeader } from "../components/shell";
export function Onboarding({
  libraryId,
  addLibrary,
  done,
}: {
  libraryId: string;
  addLibrary: () => Promise<void>;
  done: () => void;
}) {
  const [step, setStep] = useState(0);
  const [busy, setBusy] = useState(false);
  const { run } = useFeedback();
  const steps = [
    [
      "添加视频库",
      "选择一个根目录，其下应为 演员名/视频文件.mp4。",
      "添加第一个视频库",
    ],
    [
      "配置刮削器",
      "选择独立刮削器程序，需要本机 Chrome；也可以稍后配置。",
      "选择刮削器",
    ],
    ["扫描并开始", "建立索引后，到扫描页勾选条目开始刮削。", "立即扫描入库"],
  ];
  function finish() {
    localStorage.setItem("cinevault.onboarded", "1");
    done();
  }
  async function next() {
    setBusy(true);
    await run("引导操作失败", async () => {
      if (step === 0) {
        await addLibrary();
        return;
      }
      if (step === 1) {
        const path = await call("PickExecutable", "选择刮削器");
        if (!path) return;
        const cfg = await call("GetConfig");
        await call("SaveConfig", { ...cfg, scraperPath: path });
        setStep(2);
        return;
      }
      if (libraryId) await call("ImportLibrary", libraryId);
      finish();
    });
    setBusy(false);
  }
  return (
    <div className="page onboard">
      <PageHeader title="欢迎使用视频库" sub={`第 ${step + 1} / 3 步`} />
      <div className="page-body">
        <EmptyState title={steps[step][0]} desc={steps[step][1]}>
          <Button disabled={busy} onClick={() => void next()}>
            {steps[step][2]}
          </Button>
          {step === 0 && libraryId && (
            <Button onClick={() => setStep(1)}>下一步</Button>
          )}
          {step > 0 && (
            <Button
              variant="secondary"
              disabled={busy}
              onClick={() => setStep((n) => n - 1)}
            >
              上一步
            </Button>
          )}
          <Button
            variant="ghost"
            disabled={busy}
            onClick={() => (step === 1 ? setStep(2) : finish())}
          >
            {step === 1 ? "跳过，稍后配置" : "完成引导"}
          </Button>
        </EmptyState>
      </div>
    </div>
  );
}
