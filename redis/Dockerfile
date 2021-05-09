FROM redislabs/redismod:latest

RUN apt-get update &&\
    apt-get install -y python3 python3-distutils curl &&\
    apt clean

RUN curl https://bootstrap.pypa.io/get-pip.py -o get-pip.py &&\
    python3 get-pip.py

RUN echo never > /etc/rc.local

COPY ./ /redis

RUN python3 -m pip install -r /redis/requirements.txt 
