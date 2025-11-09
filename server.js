import path from 'path'
import express from 'express'
import hasher from 'crypto'

const GO_SQLITE_SERVER= process.env.GO_SQLITE_SERVER || "http://localhost:8080"
const PORT = process.env.PORT || 5000
const SQLITE_TABLE_NAME = "url_shortener"
const REDIRECTOR_PATH = process.env.REDIRECTOR_PATH || "redirect"

async function init_db(){
    try{
        const result = await db(`create?table=${SQLITE_TABLE_NAME}&col=URL+TEXT&col=sURL+TEXT`)
        console.log(result)
        return
    } catch (error){
        throw new Error(error)
    }
}

async function db(request){
    const response = await fetch(`${GO_SQLITE_SERVER}/${request}`)
    const res = await response.text()
    
    if (!response.ok){
        throw new Error(res)
    } else {
        try{
            const list = JSON.parse(res)
            return list
        } catch(error){
            return res
        }
    }
}

function validURL(url){
	return url.match(/^(https?:\/\/)?([\da-z\.-]+)\.([a-z\.]{2,6})([\/\w \.-]*)*\/?$/)
}

async function shortenURL(url){
    const datestring = (new Date()).toISOString()
    const hashobj = hasher.createHash("md5")
    hashobj.update(datestring+url)
    let hash = hashobj.digest("hex")
    hash = hash.slice(0,8)
    try{
        const result = await db(`insert?table=${SQLITE_TABLE_NAME}&val=${url}&val=${hash}`)
        console.log(result)
        return `${REDIRECTOR_PATH}/${hash}`
    } catch(error){
        throw new Error(error)
    }
}

async function fullURL(hash){
    try{
        const result = await db(`query?table=${SQLITE_TABLE_NAME}&col=sURL&val=${hash}`)
        return result[0].URL
    } catch(error){
        throw new Error(error)
    }
}

const urlShortener = express()
urlShortener.use(express.static(path.join(process.cwd(),"static")))
urlShortener.use(express.json())
urlShortener.get('/', (req, res) => {
    return res.sendFile(path.join(process.cwd(),"static","url-shortner.html"))
})
urlShortener.post('/', async (req, res) => {
    const URL = req.body.url
    if (!URL){
        return res.status(400).json({message: "Invalid URL"})
    }
    if (!validURL(URL)){
        return res.status(400).json({message: "Invalid URL"})
    }
    try{
        const sURL = await shortenURL(URL)
        console.log(`shortened url "${URL}" to "${sURL}"`)
        return res.status(200).json({message: "URL shortened", sURL: sURL})
    } catch(error){
        console.log(`while shorteneing URL ${URL} this error occured\n${error}`)
        return res.status(400).json({message: error})
    }
})
urlShortener.get(`/${REDIRECTOR_PATH}/:hash`, async (req, res) => {
    const hash = req.params.hash
    if(!hash){
        return res.sendFile(path.join(process.cwd(), "static", "invalidurl.html"))
    }
    try{
        const URL = await fullURL(hash)
        if (URL.match(/^http/))
            return res.redirect(URL)
        else
            return res.redirect("http://"+URL)
    } catch(error){
        return res.sendFile(path.join(process.cwd(), "static", "invalidurl.html"))
    }
})

init_db()
urlShortener.listen(PORT, ()=>{
    console.log(`listening on port ${PORT}`)
})