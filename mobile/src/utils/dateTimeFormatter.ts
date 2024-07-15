import { parse, format } from "date-fns"

export default function dateTimeFormatter(date: string) {   
    const parsedDate = parse(date, "EEE, dd MMM yyyy HH:mm:ss 'GMT'", new Date())
    return format(parsedDate, "yyyy-MM-dd'T'HH:mm:ssXXX")}