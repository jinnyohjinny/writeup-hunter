# Writeup Hunter

```
docker build -t writeup-hunter .
```

```
docker run -d --name writeup-hunter -e GITHUB_USERNAME=your_github_username -e GITHUB_PAT=your_personal_access_token -v /root/writeup-hunter:/app writeup-hunter
```