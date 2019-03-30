inputFile=$1
outputVersion=$RANDOM
outputDir=/pathTo/upload-server/public/results/demo/version-$outputVersion
mkdir -p $outputDir
mv $inputFile $outputDir/result.csv
# 会从stdout中读html，给前端展示
echo "success.<br> <a href='/results/demo/$outputVersion/' target='_blank'>download the result</a>"
