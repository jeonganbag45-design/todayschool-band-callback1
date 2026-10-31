export default async function handler(req,res){
  res.setHeader("Cache-Control","no-store");
  if(req.method!=="GET") return res.status(405).json({error:"Method not allowed"});
  const grade=String(req.query.grade||"2");
  const classno=String(req.query.classno||"8");
  const week=String(req.query.week||"0");
  const schoolcode=String(req.query.schoolcode||"7091455");
  const base="https://api.timefor.school/timetable";
  const tries=[
    {grade,classno,week,schoolcode,source:"comcigan"},
    {grade,classno,week,schoolcode,source:"auto"},
    {grade,classno,week,schoolcode}
  ];
  const errors=[];
  for(const params of tries){
    try{
      const url=base+"?"+new URLSearchParams(params).toString();
      const r=await fetch(url,{headers:{Accept:"application/json","User-Agent":"TodaySchool-Mobile/1.0"}});
      const text=await r.text();
      if(!r.ok){errors.push(`${r.status} ${text.slice(0,180)}`);continue}
      let data;
      try{data=JSON.parse(text)}catch(e){errors.push("Invalid JSON");continue}
      return res.status(200).json({ok:true,data,source:url});
    }catch(e){errors.push(e.message)}
  }
  return res.status(502).json({ok:false,error:"시간표 원본 API 연결 실패",details:errors});
}